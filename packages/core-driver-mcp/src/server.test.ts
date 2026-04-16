import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { FakeClock, NoopLogger } from "@atomyx/core-driver";
import { MockDriver, node } from "@atomyx/core-driver/testing";
import { Roles, AttrKeys } from "@atomyx/core-driver";
import { createMcpServer } from "./server.js";
import { DeviceSession } from "./device-session.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

/**
 * End-to-end MCP server tests using an in-memory Orchestra +
 * MockDriver. We exercise the full path: tools/list response
 * shape, tools/call validation, tool dispatch, error handling,
 * Orchestra→Driver wire-up.
 *
 * No transport is connected — we drive the Server's request
 * handlers directly via Server's internal `_requestHandlers`
 * map, which is what `setRequestHandler` populates. The MCP SDK
 * dispatches requests through this map when a transport
 * delivers them, so calling them directly is equivalent to a
 * full transport round-trip minus the JSON-RPC framing.
 */

interface InternalServer {
  _requestHandlers: Map<string, (req: unknown, extra: unknown) => Promise<unknown>>;
}

function loginScreen() {
  return node({
    role: Roles.Container,
    bounds: "0,0,430,932",
    children: [
      node({
        role: Roles.Button,
        id: "login_btn",
        text: "Sign in",
        label: "Login button",
        bounds: "100,400,330,460",
        enabled: true,
        clickable: true,
      }),
      node({
        role: Roles.TextField,
        id: "email",
        hint: "Email address",
        bounds: "40,300,390,340",
        enabled: true,
      }),
    ],
  });
}

async function buildServer() {
  const driver = new MockDriver();
  driver.stageHierarchyRepeated(loginScreen(), 100);
  const logger = new NoopLogger();
  const clock = new FakeClock();
  // DeviceSession takes a factory map; tests use MockDriver for
  // both platforms so the same driver instance is returned
  // regardless of which platform the test passes to select().
  const session = new DeviceSession({
    factories: {
      ios: () => driver,
      android: () => driver,
    },
    clock,
    logger,
  });
  // Pre-bind the mock device so existing tests that immediately
  // call device-touching tools don't need a `select_device` setup
  // step. A "mock-device" id + "android" platform is arbitrary —
  // tests that exercise multi-device flows can call session.select
  // again.
  await session.select({ platform: "android", id: "mock-device" });
  const server = createMcpServer({ session, logger, clock });
  return { server, driver, session };
}

async function callTool(
  server: ReturnType<typeof createMcpServer>,
  name: string,
  args: Record<string, unknown>,
): Promise<{ content: Array<{ type: string; text: string; data?: string; mimeType?: string }>; isError?: boolean }> {
  // The MCP SDK populates _requestHandlers with method-name
  // keys, but uses the schema's `method` literal as the key.
  // CallToolRequestSchema's method is "tools/call".
  const handler = (server as unknown as InternalServer)._requestHandlers.get(
    CallToolRequestSchema.shape.method.value,
  );
  if (!handler) throw new Error("tools/call handler not registered");
  const response = (await handler(
    {
      method: "tools/call",
      params: { name, arguments: args },
    },
    {},
  )) as { content: Array<{ type: string; text: string }>; isError?: boolean };
  return response;
}

async function listTools(
  server: ReturnType<typeof createMcpServer>,
): Promise<{ tools: Array<{ name: string; description: string; inputSchema: unknown }> }> {
  const handler = (server as unknown as InternalServer)._requestHandlers.get(
    ListToolsRequestSchema.shape.method.value,
  );
  if (!handler) throw new Error("tools/list handler not registered");
  return (await handler({ method: "tools/list", params: {} }, {})) as {
    tools: Array<{ name: string; description: string; inputSchema: unknown }>;
  };
}

describe("createMcpServer / tools/list", () => {
  it("lists all DEFAULT_TOOLS with name + description + JSON schema", async () => {
    const { server } = await buildServer();
    const result = await listTools(server);
    assert.ok(result.tools.length >= 9);
    const names = result.tools.map((t) => t.name);
    assert.ok(names.includes("launch_app"));
    assert.ok(names.includes("get_ui_tree"));
    assert.ok(names.includes("find_element"));
    assert.ok(names.includes("tap"));
    assert.ok(names.includes("input_text"));
    assert.ok(names.includes("swipe"));
    assert.ok(names.includes("press_key"));
    assert.ok(names.includes("screenshot"));
    assert.ok(names.includes("wait_for_element"));
    // Every tool has a JSON schema with `type: "object"`.
    for (const t of result.tools) {
      const schema = t.inputSchema as { type?: string };
      assert.equal(schema.type, "object", `${t.name} schema must have type:"object"`);
    }
  });
});

describe("tools/call dispatch — happy path", () => {
  it("launch_app dispatches to Orchestra.launchApp", async () => {
    const { server, driver } = await buildServer();
    const result = await callTool(server, "launch_app", { appId: "com.example.app" });
    assert.equal(result.isError, undefined);
    const launches = driver.calls.filter((c) => c.method === "launchApp");
    assert.equal(launches.length, 1);
    assert.equal(launches[0]!.args[0], "com.example.app");
  });

  it("find_element returns element details", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "find_element", { id: "login_btn" });
    assert.equal(result.isError, undefined);
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.found, true);
    assert.equal(parsed.id, "login_btn");
    assert.equal(parsed.text, "Sign in");
    assert.equal(parsed.role, "button");
    assert.deepEqual(parsed.center, { x: 215, y: 430 });
  });

  it("find_element returns found:false when no match", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "find_element", { id: "nope" });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.found, false);
  });

  it("tap with selector dispatches the full pipeline", async () => {
    const { server, driver } = await buildServer();
    const result = await callTool(server, "tap", {
      selector: { id: "login_btn" },
    });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, true);
    assert.equal(parsed.resolvedBy, "id");
    const taps = driver.calls.filter((c) => c.method === "tap");
    assert.equal(taps.length, 1);
    const point = taps[0]!.args[0] as { x: number; y: number };
    assert.equal(point.x, 215);
    assert.equal(point.y, 430);
  });

  it("tap with coordinates bypasses the selector pipeline", async () => {
    const { server, driver } = await buildServer();
    const result = await callTool(server, "tap", { x: 100, y: 200 });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, true);
    const taps = driver.calls.filter((c) => c.method === "tap");
    assert.equal(taps.length, 1);
  });

  it("get_ui_tree returns flat node list with depth", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "get_ui_tree", {});
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.total, 3); // root + button + textfield
    assert.equal(parsed.nodes.length, 3);
    assert.equal(parsed.nodes[0]!.depth, 0);
    assert.equal(parsed.nodes[1]!.depth, 1);
    assert.equal(parsed.nodes[2]!.depth, 1);
  });

  it("get_ui_tree honors limit", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "get_ui_tree", { limit: 2 });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.returned, 2);
    assert.equal(parsed.truncated, true);
  });

  it("swipe direction dispatches Orchestra.swipeDirection", async () => {
    const { server, driver } = await buildServer();
    await callTool(server, "swipe", { direction: "up" });
    assert.equal(driver.calls.filter((c) => c.method === "swipe").length, 1);
  });

  it("swipe coordinates dispatches Orchestra.swipeAt", async () => {
    const { server, driver } = await buildServer();
    await callTool(server, "swipe", { fromX: 10, fromY: 20, toX: 30, toY: 40 });
    assert.equal(driver.calls.filter((c) => c.method === "swipe").length, 1);
  });

  it("press_key returns ActionResult", async () => {
    const { server, driver } = await buildServer();
    const result = await callTool(server, "press_key", { key: "back" });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, true);
    assert.equal(driver.calls.filter((c) => c.method === "pressKey").length, 1);
  });

  it("screenshot saves file and returns path", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "screenshot", {});
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(typeof parsed.path, "string");
    assert.ok(parsed.path.includes(".atomyx/screenshots/screenshot-"));
    assert.equal(typeof parsed.sizeBytes, "number");
    assert.equal(parsed.format, "png"); // MockDriver returns PNG
  });

  it("wait_for_element returns find_element-shaped success payload", async () => {
    // Regression test for the Batch 15 audit finding: wait_for_element
    // used to return only {id, text, label, role, bounds: rawString}
    // — agents had to call find_element again to get `center`,
    // `enabled`, `clickable`, and `value`. Normalize to the
    // find_element shape so poll-then-use-coordinates is one call.
    const { server } = await buildServer();
    const result = await callTool(server, "wait_for_element", {
      selector: { id: "login_btn" },
      timeoutMs: 1000,
    });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.found, true);
    assert.equal(parsed.id, "login_btn");
    assert.equal(parsed.text, "Sign in");
    assert.equal(parsed.label, "Login button");
    assert.equal(parsed.role, "button");
    // bounds is now parsed (Bounds object), not a raw "l,t,r,b" string.
    assert.equal(typeof parsed.bounds, "object");
    assert.equal(parsed.bounds.left, 100);
    assert.equal(parsed.bounds.right, 330);
    // center is provided so agents can pass it straight to tap({x,y}).
    assert.deepEqual(parsed.center, { x: 215, y: 430 });
    // enabled + clickable flags come through.
    assert.equal(parsed.enabled, true);
    assert.equal(parsed.clickable, true);
  });

  // Note: no timeout-path test here. `buildServer` injects a
  // `FakeClock` into Orchestra's Finder, which means `waitFor` on a
  // never-matching selector would block forever waiting on a
  // `clock.sleep` that never resolves. Driving the clock forward in
  // parallel is doable (see the `tap_and_wait_transition` FakeClock
  // test in new-tools.test.ts for the pattern) but overkill here —
  // the positive test above proves the shape normalization which is
  // the actual Batch 15 audit fix.
});

describe("tools/call validation + errors", () => {
  it("rejects unknown tool with isError", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "nope", {});
    assert.equal(result.isError, true);
    assert.match(result.content[0]!.text, /Unknown tool/);
  });

  it("rejects invalid arguments with isError", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "launch_app", {});
    assert.equal(result.isError, true);
    assert.match(result.content[0]!.text, /Invalid arguments/);
  });

  it("input_text refines: must have selector OR x+y", async () => {
    const { server } = await buildServer();
    const result = await callTool(server, "input_text", { text: "hi" });
    assert.equal(result.isError, true);
  });
});

describe("tools/call when Orchestra returns ok:false", () => {
  it("propagates the ActionResult shape verbatim", async () => {
    // Build a server with a target that's obscured by a modal.
    const driver = new MockDriver();
    const obscuredTree = node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      children: [
        node({
          role: Roles.Cell,
          id: "target",
          bounds: "100,200,300,260",
          enabled: true,
        }),
        node({
          role: Roles.Dialog,
          id: "confirm-sheet",
          label: "Confirm",
          bounds: "0,100,430,600",
        }),
      ],
    });
    driver.stageHierarchyRepeated(obscuredTree, 5);
    const logger = new NoopLogger();
    const clock = new FakeClock();
    const session = new DeviceSession({
      factories: { ios: () => driver, android: () => driver },
      clock,
      logger,
    });
    await session.select({ platform: "android", id: "mock" });
    const server = createMcpServer({ session, logger, clock });

    const result = await callTool(server, "tap", {
      selector: { id: "target" },
    });
    const parsed = JSON.parse(result.content[0]!.text);
    assert.equal(parsed.ok, false);
    assert.match(parsed.reason, /obscured/);
    assert.equal(parsed.obscurer.id, "confirm-sheet");
    // Driver tap should NOT have been dispatched.
    assert.equal(driver.calls.filter((c) => c.method === "tap").length, 0);
    // Avoid unused import warning
    void AttrKeys;
  });
});

describe("custom tool surface + duplicate detection", () => {
  it("createMcpServer accepts a custom tools list", async () => {
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(loginScreen(), 5);
    const session = new DeviceSession({
      factories: { ios: () => driver, android: () => driver },
      clock: new FakeClock(),
    });
    await session.select({ platform: "android", id: "mock" });
    const server = createMcpServer({
      session,
      tools: [], // empty surface
    });
    const result = await listTools(server);
    assert.equal(result.tools.length, 0);
    void driver; // silence unused
  });

  it("throws on duplicate tool names", () => {
    const session = new DeviceSession({
      factories: { ios: () => new MockDriver(), android: () => new MockDriver() },
    });
    assert.throws(() =>
      createMcpServer({
        session,
        tools: [
          {
            name: "dup",
            description: "",
            inputSchema: { parse: () => ({}), safeParse: () => ({ success: true, data: {} }) } as never,
            execute: async () => ({}),
          },
          {
            name: "dup",
            description: "",
            inputSchema: { parse: () => ({}), safeParse: () => ({ success: true, data: {} }) } as never,
            execute: async () => ({}),
          },
        ],
      }),
    );
  });
});
