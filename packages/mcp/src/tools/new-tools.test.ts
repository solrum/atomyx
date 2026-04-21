import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  FakeClock,
  NoopLogger,
  InMemoryStorage,
  RunStore,
} from "@atomyx/driver";
import { MockDriver, node } from "@atomyx/driver/testing";
import { Roles } from "@atomyx/driver";
import { createMcpServer } from "../server.js";
import { DeviceSession } from "../device-session.js";
import { CallToolRequestSchema } from "@modelcontextprotocol/sdk/types.js";

/**
 * Smoke tests for the ported tool surface:
 *   - tap_and_wait_transition
 *   - start_run / finish_run / report_bug (lifecycle)
 *   - list_runs / get_run / list_bugs / get_bug (read-only queries)
 *   - add_case_study / get_case_studies
 *   - list_apps / list_devices
 *
 * Each test dispatches through the MCP `tools/call` handler with an
 * in-memory Storage + RunStore so the tests stay hermetic (no
 * ~/.atomyx writes).
 */

interface InternalServer {
  _requestHandlers: Map<string, (req: unknown, extra: unknown) => Promise<unknown>>;
}

async function buildServer(opts: { sharedClock?: FakeClock } = {}) {
  const driver = new MockDriver();
  driver.stageHierarchyRepeated(
    node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      children: [
        node({
          role: Roles.Button,
          id: "login",
          text: "Sign in",
          bounds: "100,400,330,460",
          enabled: true,
          clickable: true,
        }),
      ],
    }),
    100,
  );
  const logger = new NoopLogger();
  // Share one FakeClock across DeviceSession and createMcpServer when
  // the caller wants deterministic fast-forward. Otherwise give each
  // its own.
  const sessionClock = opts.sharedClock ?? new FakeClock();
  const session = new DeviceSession({
    factories: {
      ios: () => driver,
      android: () => driver,
    },
    clock: sessionClock,
    logger,
  });
  // Pre-bind the mock device so tests that immediately call device-
  // touching tools don't need an extra select_device step.
  await session.select({ platform: "android", id: "mock-device" });
  const storage = new InMemoryStorage();
  const runStore = new RunStore();
  const server = createMcpServer({
    session,
    storage,
    runStore,
    logger,
    clock: opts.sharedClock,
  });
  return { server, driver, storage, runStore, clock: sessionClock, session };
}

async function callTool(
  server: ReturnType<typeof createMcpServer>,
  name: string,
  args: Record<string, unknown>,
) {
  const handler = (server as unknown as InternalServer)._requestHandlers.get(
    CallToolRequestSchema.shape.method.value,
  );
  if (!handler) throw new Error("tools/call handler not registered");
  return (await handler(
    { method: "tools/call", params: { name, arguments: args } },
    {},
  )) as { content: Array<{ type: string; text: string }>; isError?: boolean };
}

describe("run lifecycle tools", () => {
  it("start_run creates an active run", async () => {
    const { server, runStore } = await buildServer();
    const result = await callTool(server, "start_run", {
      name: "login flow",
      source: "exploratory",
    });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, true);
    assert.ok(parsed.id);
    assert.equal(runStore.current()?.name, "login flow");
  });

  it("finish_run persists the record to storage", async () => {
    const { server, storage } = await buildServer();
    await callTool(server, "start_run", { name: "x" });
    const finish = await callTool(server, "finish_run", {
      status: "passed",
      summary: "all good",
    });
    const parsed = JSON.parse(finish.content[0]!.text);
    assert.equal(parsed.ok, true);
    assert.equal(parsed.status, "passed");
    const keys = await storage.list("runs");
    assert.equal(keys.length, 1);
  });

  it("finish_run returns ok:false when no run active", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "finish_run", {});
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, false);
  });

  it("finish_run twice in a row returns ok:false on the second call", async () => {
    const { server } = await buildServer();
    await callTool(server, "start_run", { name: "x" });
    const first = JSON.parse(
      (await callTool(server, "finish_run", {})).content[0]!.text,
    );
    assert.equal(first.ok, true);
    const second = JSON.parse(
      (await callTool(server, "finish_run", {})).content[0]!.text,
    );
    assert.equal(second.ok, false);
  });

  it("start_run twice force-closes the predecessor and persists it", async () => {
    const { server, storage } = await buildServer();
    const first = JSON.parse(
      (await callTool(server, "start_run", { name: "first" })).content[0]!.text,
    );
    const second = JSON.parse(
      (await callTool(server, "start_run", { name: "second" })).content[0]!
        .text,
    );
    assert.equal(second.ok, true);
    assert.equal(second.erroredPredecessor, first.id);

    // The force-errored predecessor is persisted to runs/<id> with
    // status="error" so agents that forgot finish_run still leave a
    // trail.
    const persisted = await storage.load<{ status: string; summary: string }>(
      `runs/${first.id}`,
    );
    assert.ok(persisted);
    assert.equal(persisted!.status, "error");
    assert.match(persisted!.summary, /force-closed/);
  });
});

describe("run read tools", () => {
  it("list_runs returns persisted runs sorted desc by startedAt", async () => {
    const { server } = await buildServer();
    await callTool(server, "start_run", { name: "run-a" });
    await callTool(server, "finish_run", { status: "passed" });
    await new Promise((r) => setTimeout(r, 5));
    await callTool(server, "start_run", { name: "run-b" });
    await callTool(server, "finish_run", { status: "failed" });

    const result = JSON.parse(
      (await callTool(server, "list_runs", {})).content[0]!.text,
    );
    assert.equal(result.ok, true);
    assert.equal(result.count, 2);
    // Most recent first.
    assert.equal(result.runs[0]!.name, "run-b");
    assert.equal(result.runs[1]!.name, "run-a");
    // Summaries include key fields but not full findings array.
    assert.equal(result.runs[0]!.status, "failed");
    assert.ok("findingsCount" in result.runs[0]!);
    assert.ok("durationMs" in result.runs[0]!);
  });

  it("list_runs filters by status", async () => {
    const { server } = await buildServer();
    await callTool(server, "start_run", { name: "passing" });
    await callTool(server, "finish_run", { status: "passed" });
    await callTool(server, "start_run", { name: "failing" });
    await callTool(server, "finish_run", { status: "failed" });

    const result = JSON.parse(
      (await callTool(server, "list_runs", { status: "failed" })).content[0]!
        .text,
    );
    assert.equal(result.count, 1);
    assert.equal(result.runs[0]!.name, "failing");
    assert.equal(result.runs[0]!.status, "failed");
  });

  it("list_runs honors limit", async () => {
    const { server } = await buildServer();
    for (let i = 0; i < 3; i++) {
      await callTool(server, "start_run", { name: `run-${i}` });
      await callTool(server, "finish_run", {});
    }
    const result = JSON.parse(
      (await callTool(server, "list_runs", { limit: 2 })).content[0]!.text,
    );
    assert.equal(result.count, 2);
    assert.equal(result.totalMatching, 3);
  });

  it("get_run round-trips the full persisted record", async () => {
    const { server } = await buildServer();
    const start = JSON.parse(
      (await callTool(server, "start_run", { name: "round-trip" }))
        .content[0]!.text,
    );
    await callTool(server, "finish_run", {
      status: "passed",
      summary: "all green",
    });

    const result = JSON.parse(
      (await callTool(server, "get_run", { id: start.id })).content[0]!.text,
    );
    assert.equal(result.ok, true);
    assert.equal(result.run.id, start.id);
    assert.equal(result.run.name, "round-trip");
    assert.equal(result.run.status, "passed");
    assert.equal(result.run.summary, "all green");
  });

  it("get_run returns ok:false when id is not found", async () => {
    const { server } = await buildServer();
    const result = JSON.parse(
      (await callTool(server, "get_run", { id: "run-nope-999" })).content[0]!
        .text,
    );
    assert.equal(result.ok, false);
    assert.match(result.reason, /not found/);
  });

  it("list_runs honors offset for pagination", async () => {
    const { server } = await buildServer();
    for (let i = 0; i < 5; i++) {
      await callTool(server, "start_run", { name: `run-${i}` });
      await callTool(server, "finish_run", {});
    }
    const page1 = JSON.parse(
      (await callTool(server, "list_runs", { limit: 2, offset: 0 })).content[0]!
        .text,
    );
    assert.equal(page1.count, 2);
    assert.equal(page1.totalMatching, 5);
    assert.equal(page1.offset, 0);
    assert.equal(page1.nextOffset, 2);

    const page2 = JSON.parse(
      (await callTool(server, "list_runs", { limit: 2, offset: 2 })).content[0]!
        .text,
    );
    assert.equal(page2.count, 2);
    assert.equal(page2.offset, 2);
    assert.equal(page2.nextOffset, 4);

    const page3 = JSON.parse(
      (await callTool(server, "list_runs", { limit: 2, offset: 4 })).content[0]!
        .text,
    );
    assert.equal(page3.count, 1);
    assert.equal(page3.nextOffset, null);
  });
});

describe("run mutations", () => {
  it("update_run_summary replaces the summary without touching other fields", async () => {
    const { server } = await buildServer();
    const start = JSON.parse(
      (await callTool(server, "start_run", { name: "x" })).content[0]!.text,
    );
    await callTool(server, "finish_run", {
      status: "passed",
      summary: "initial",
    });

    const update = JSON.parse(
      (
        await callTool(server, "update_run_summary", {
          id: start.id,
          summary: "revised after investigation",
        })
      ).content[0]!.text,
    );
    assert.equal(update.ok, true);

    const reloaded = JSON.parse(
      (await callTool(server, "get_run", { id: start.id })).content[0]!.text,
    );
    assert.equal(reloaded.run.summary, "revised after investigation");
    // Other fields preserved.
    assert.equal(reloaded.run.status, "passed");
    assert.equal(reloaded.run.name, "x");
  });

  it("update_run_summary returns ok:false for missing id", async () => {
    const { server } = await buildServer();
    const result = JSON.parse(
      (
        await callTool(server, "update_run_summary", {
          id: "run-nope",
          summary: "x",
        })
      ).content[0]!.text,
    );
    assert.equal(result.ok, false);
  });

  it("delete_run removes the run record", async () => {
    const { server, storage } = await buildServer();
    const start = JSON.parse(
      (await callTool(server, "start_run", { name: "to-delete" })).content[0]!
        .text,
    );
    await callTool(server, "finish_run", {});

    const del = JSON.parse(
      (await callTool(server, "delete_run", { id: start.id })).content[0]!.text,
    );
    assert.equal(del.ok, true);

    const list = await storage.list("runs");
    assert.equal(list.length, 0);

    const get = JSON.parse(
      (await callTool(server, "get_run", { id: start.id })).content[0]!.text,
    );
    assert.equal(get.ok, false);
  });

  it("delete_run returns ok:false for missing id (no side effects)", async () => {
    const { server } = await buildServer();
    const result = JSON.parse(
      (await callTool(server, "delete_run", { id: "run-nope" })).content[0]!
        .text,
    );
    assert.equal(result.ok, false);
  });
});

describe("bug read tools", () => {
  it("list_bugs returns persisted bugs filtered by runId", async () => {
    const { server } = await buildServer();
    const runA = JSON.parse(
      (await callTool(server, "start_run", { name: "a" })).content[0]!.text,
    );
    await callTool(server, "report_bug", {
      title: "bug-a1",
      description: "x",
      captureScreenshot: false,
    });
    await callTool(server, "report_bug", {
      title: "bug-a2",
      description: "y",
      captureScreenshot: false,
    });
    await callTool(server, "finish_run", {});

    const runB = JSON.parse(
      (await callTool(server, "start_run", { name: "b" })).content[0]!.text,
    );
    await callTool(server, "report_bug", {
      title: "bug-b1",
      description: "z",
      captureScreenshot: false,
    });
    await callTool(server, "finish_run", {});

    // Full list should have 3 bugs.
    const all = JSON.parse(
      (await callTool(server, "list_bugs", {})).content[0]!.text,
    );
    assert.equal(all.count, 3);

    // Filter to run A should have 2 bugs.
    const onlyA = JSON.parse(
      (await callTool(server, "list_bugs", { runId: runA.id })).content[0]!
        .text,
    );
    assert.equal(onlyA.count, 2);
    assert.ok(onlyA.bugs.every((b: { runId: string }) => b.runId === runA.id));

    // Filter to run B should have 1 bug.
    const onlyB = JSON.parse(
      (await callTool(server, "list_bugs", { runId: runB.id })).content[0]!
        .text,
    );
    assert.equal(onlyB.count, 1);
    assert.equal(onlyB.bugs[0]!.title, "bug-b1");
  });

  it("list_bugs excludes nested screenshot keys from the result", async () => {
    // When `report_bug` captures a screenshot it writes two keys to
    // storage: bugs/<id> and bugs/<id>/screenshot. The list tool must
    // return one entry per bug, not two.
    const { server } = await buildServer();
    await callTool(server, "start_run", { name: "with screenshots" });
    await callTool(server, "report_bug", {
      title: "bug with screenshot",
      description: "x",
      captureScreenshot: true,
    });

    const result = JSON.parse(
      (await callTool(server, "list_bugs", {})).content[0]!.text,
    );
    assert.equal(result.count, 1);
    assert.equal(result.bugs[0]!.hasScreenshot, true);
  });

  it("get_bug round-trips the full persisted record", async () => {
    const { server } = await buildServer();
    await callTool(server, "start_run", { name: "r" });
    const reported = JSON.parse(
      (
        await callTool(server, "report_bug", {
          title: "login button no-op",
          description: "tapping login does nothing",
          captureScreenshot: false,
        })
      ).content[0]!.text,
    );

    const result = JSON.parse(
      (await callTool(server, "get_bug", { id: reported.id })).content[0]!
        .text,
    );
    assert.equal(result.ok, true);
    assert.equal(result.bug.id, reported.id);
    assert.equal(result.bug.title, "login button no-op");
    assert.equal(result.bug.description, "tapping login does nothing");
  });

  it("get_bug returns ok:false when id is not found", async () => {
    const { server } = await buildServer();
    const result = JSON.parse(
      (await callTool(server, "get_bug", { id: "bug-nope-999" })).content[0]!
        .text,
    );
    assert.equal(result.ok, false);
    assert.match(result.reason, /not found/);
  });

  it("delete_bug removes both the record and its screenshot sibling", async () => {
    const { server, storage } = await buildServer();
    await callTool(server, "start_run", { name: "r" });
    const reported = JSON.parse(
      (
        await callTool(server, "report_bug", {
          title: "with screenshot",
          description: "evidence captured",
          captureScreenshot: true,
        })
      ).content[0]!.text,
    );

    // Both keys should exist before delete.
    const before = await storage.list("bugs");
    assert.ok(before.includes(`bugs/${reported.id}`));
    assert.ok(before.includes(`bugs/${reported.id}/screenshot`));

    const del = JSON.parse(
      (await callTool(server, "delete_bug", { id: reported.id })).content[0]!
        .text,
    );
    assert.equal(del.ok, true);

    const after = await storage.list("bugs");
    assert.equal(after.length, 0);
  });

  it("delete_bug is safe when no screenshot exists", async () => {
    const { server } = await buildServer();
    await callTool(server, "start_run", { name: "r" });
    const reported = JSON.parse(
      (
        await callTool(server, "report_bug", {
          title: "no screenshot",
          description: "x",
          captureScreenshot: false,
        })
      ).content[0]!.text,
    );
    const del = JSON.parse(
      (await callTool(server, "delete_bug", { id: reported.id })).content[0]!
        .text,
    );
    assert.equal(del.ok, true);
  });

  it("delete_bug returns ok:false for missing id", async () => {
    const { server } = await buildServer();
    const result = JSON.parse(
      (await callTool(server, "delete_bug", { id: "bug-nope" })).content[0]!
        .text,
    );
    assert.equal(result.ok, false);
  });

  it("list_bugs honors offset for pagination", async () => {
    const { server } = await buildServer();
    await callTool(server, "start_run", { name: "r" });
    for (let i = 0; i < 4; i++) {
      await callTool(server, "report_bug", {
        title: `bug-${i}`,
        description: "x",
        captureScreenshot: false,
      });
    }
    const page1 = JSON.parse(
      (await callTool(server, "list_bugs", { limit: 2, offset: 0 })).content[0]!
        .text,
    );
    assert.equal(page1.count, 2);
    assert.equal(page1.totalMatching, 4);
    assert.equal(page1.nextOffset, 2);
    const page2 = JSON.parse(
      (await callTool(server, "list_bugs", { limit: 2, offset: 2 })).content[0]!
        .text,
    );
    assert.equal(page2.count, 2);
    assert.equal(page2.nextOffset, null);
  });
});

describe("report_bug", () => {
  it("records a bug under the active run", async () => {
    const { server, storage, runStore } = await buildServer();
    await callTool(server, "start_run", { name: "session" });
    const result = await callTool(server, "report_bug", {
      title: "Login button no-op",
      description: "Tapping login does nothing after 5s",
      captureScreenshot: false,
    });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, true);
    assert.ok(parsed.id);
    assert.equal(runStore.current()!.findings.length, 1);
    const keys = await storage.list("bugs");
    assert.ok(keys.some((k) => k.startsWith("bugs/")));
  });

  it("returns ok:false when no active run", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "report_bug", {
      title: "x",
      description: "y",
      captureScreenshot: false,
    });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, false);
  });
});

describe("case study tools", () => {
  it("add_case_study + get_case_studies round-trip", async () => {
    const { server } = await buildServer();
    const add = await callTool(server, "add_case_study", {
      title: "Obscurer dismissal",
      trigger: "Tap blocked by modal",
      solution: "Find obscurer by label then tap dismiss",
    });
    const addParsed = JSON.parse(add.content[0]!.text);
    assert.equal(addParsed.ok, true);
    assert.match(addParsed.key, /case-studies\/\d{4}-\d{2}\/obscurer-dismissal/);

    const list = await callTool(server, "get_case_studies", {});
    const listParsed = JSON.parse(list.content[0]!.text);
    assert.equal(listParsed.count, 1);
    assert.match(listParsed.studies[0]!.body, /Obscurer dismissal/);
  });

  it("get_case_studies returns empty list when none exist", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "get_case_studies", {});
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.count, 0);
    assert.deepEqual(parsed.studies, []);
  });
});

describe("list_apps", () => {
  it("calls orchestra.listApps and returns the result", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "list_apps", {});
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.count, 0);
    assert.ok(parsed.note);
  });
});

describe("list_devices", () => {
  it("runs without throwing even when adb/xcrun missing", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "list_devices", { platform: "all" });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.ok("count" in parsed);
    assert.ok(Array.isArray(parsed.devices));
  });
});

describe("tap_and_wait_transition", () => {
  it("returns ok:true when anchor appears in the new state", async () => {
    // The MockDriver keeps returning the same tree where login
    // button exists. With waitForAppear on the login anchor, the
    // first poll should succeed.
    const { server } = await buildServer();
    const result = await callTool(server, "tap_and_wait_transition", {
      selector: { id: "login" },
      waitForAppear: { id: "login" },
      timeoutMs: 2000,
      intervalMs: 50,
    });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, true);
  });

  it("reports classification on timeout", async () => {
    // waitForAbsent on an id that is never absent → timeout + classify.
    // Use a very short timeout to keep the test fast.
    const { server } = await buildServer();
    const result = await callTool(server, "tap_and_wait_transition", {
      selector: { id: "login" },
      waitForAbsent: { id: "login" },
      timeoutMs: 200,
      maxTimeoutMs: 200,
      intervalMs: 50,
    });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, false);
    assert.ok(parsed.classification);
  });

  it("uses ctx.clock for polling — FakeClock makes the tool deterministic", async () => {
    // The tool polls via ctx.clock, so a shared FakeClock injected
    // into both Orchestra AND createMcpServer lets tests fast-forward
    // completely without wall-clock delays.
    //
    // The test drives the clock forward in a microtask-yielding loop
    // to simulate real polling: each iteration flushes pending
    // microtasks (letting the tool reach its next clock.sleep call)
    // then advances past that sleep's interval. A 10-iteration budget
    // is enough to let the first or second poll find the "login"
    // anchor via waitForAppear.
    const fakeClock = new FakeClock();
    const { server } = await buildServer({ sharedClock: fakeClock });

    const pending = callTool(server, "tap_and_wait_transition", {
      selector: { id: "login" },
      waitForAppear: { id: "login" },
      // Intervals that would take seconds of real time under
      // SystemClock — FakeClock collapses this to zero wall-clock.
      timeoutMs: 30_000,
      intervalMs: 2_000,
    });

    // Drive the clock forward in lock-step with the tool's awaits.
    const realStart = Date.now();
    for (let i = 0; i < 10; i++) {
      await new Promise((r) => setImmediate(r));
      fakeClock.advance(2_000);
    }
    const result = await pending;
    const realElapsed = Date.now() - realStart;

    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, true);
    // Determinism check: under SystemClock the 2s interval would
    // force multiple seconds of real delay. Under FakeClock we
    // should be under 200ms wall-clock.
    assert.ok(
      realElapsed < 500,
      `expected <500ms wall-clock, got ${realElapsed}ms (Date.now/setTimeout leak?)`,
    );
  });
});

describe("DEFAULT_TOOLS registry", () => {
  it("tools/list surfaces all 27 tools", async () => {
    const { server } = await buildServer();
    // Use ListToolsRequestSchema via the internal handler.
    const handler = (server as unknown as InternalServer)._requestHandlers.get(
      "tools/list",
    );
    const result = (await handler!({ method: "tools/list", params: {} }, {})) as {
      tools: Array<{ name: string }>;
    };
    const names = result.tools.map((t) => t.name);
    for (const expected of [
      "list_devices",
      "select_device",
      "disconnect_device",
      "list_apps",
      "launch_app",
      "get_ui_tree",
      "find_element",
      "tap",
      "tap_and_wait_transition",
      "input_text",
      "swipe",
      "press_key",
      "screenshot",
      "wait_for_element",
      "start_run",
      "finish_run",
      "list_runs",
      "get_run",
      "update_run_summary",
      "delete_run",
      "report_bug",
      "list_bugs",
      "get_bug",
      "delete_bug",
      "add_case_study",
      "get_case_studies",
      "run_script",
    ]) {
      assert.ok(names.includes(expected), `missing tool: ${expected}`);
    }
    assert.equal(names.length, 27);
  });
});
