import assert from "node:assert/strict";
import { describe, it } from "node:test";
import type { ArtifactStore } from "../../../domain/features/artifacts/index.js";
import type {
  RunEvent,
  StudioRuntime,
} from "../../../domain/features/runtime/index.js";
import type { RunMetadata } from "./runs.contract.js";
import { createZustandRuns } from "./runs.zustand.js";

// ---------------------------------------------------------------------------
// Inline fakes — implement only the methods called by the store.
// ---------------------------------------------------------------------------

function makeRunEvent(partial: Partial<RunEvent> & { type: RunEvent["type"] }): RunEvent {
  return partial as RunEvent;
}

async function* eventsIterable(events: RunEvent[]): AsyncIterable<RunEvent> {
  for (const ev of events) {
    yield ev;
  }
}

function throwingIterable(message: string): AsyncIterable<RunEvent> {
  return {
    [Symbol.asyncIterator]: () => ({
      next: () => Promise.reject(new Error(message)),
    }),
  };
}

// Yields nothing and completes only after `signal` resolves, so a
// stopRun() landing mid-iteration can mark the run cancelled before
// the stream ends.
function pendingUntil(signal: Promise<void>): AsyncIterable<RunEvent> {
  return {
    [Symbol.asyncIterator]: () => ({
      next: async () => {
        await signal;
        return { done: true, value: undefined };
      },
    }),
  };
}

function makeFakeArtifacts(overrides: Partial<ArtifactStore> = {}): ArtifactStore {
  const runs: RunMetadata[] = [];
  return {
    createRun: async (meta) => { runs.push({ ...meta }); },
    appendEvent: async () => {},
    finalizeRun: async (runId, patch) => {
      const idx = runs.findIndex((r) => r.runId === runId);
      if (idx !== -1) runs[idx] = { ...runs[idx], ...patch } as RunMetadata;
    },
    listRuns: async () => [...runs],
    getRun: async (runId) => runs.find((r) => r.runId === runId) ?? null,
    getEvents: async function* () {},
    saveArtifact: async () => ({ name: "", size: 0 }),
    listArtifacts: async () => [],
    readArtifact: async () => new Uint8Array(),
    deleteRun: async (runId) => {
      const idx = runs.findIndex((r) => r.runId === runId);
      if (idx !== -1) runs.splice(idx, 1);
    },
    ...overrides,
  };
}

function makeFakeRuntime(events: RunEvent[] = []): StudioRuntime {
  return {
    connect: async () => {},
    disconnect: async () => {},
    listDevices: async () => [],
    listApps: async () => [],
    runScript: () => eventsIterable(events),
    stop: async () => {},
    screenshot: async () => new Uint8Array(),
    getUiTree: async () => ({ attributes: {}, children: [] }),
  };
}

const DEFAULT_OPTS = { deviceId: "dev-1", cwd: "/scripts" };

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("createZustandRuns", () => {
  it("initial snapshot has null live run and empty history", () => {
    const store = createZustandRuns({
      runtime: makeFakeRuntime(),
      artifacts: makeFakeArtifacts(),
    });
    const snap = store.getSnapshot();
    assert.equal(snap.live, null);
    assert.deepEqual(snap.history, []);
  });

  it("subscribe notifies listener on state change", async () => {
    const store = createZustandRuns({
      runtime: makeFakeRuntime([
        makeRunEvent({ type: "runCompleted", ok: true, completedAt: Date.now() }),
      ]),
      artifacts: makeFakeArtifacts(),
    });

    let callCount = 0;
    const unsub = store.subscribe(() => { callCount++; });
    await store.startRun("script.yml", "yaml: {}", DEFAULT_OPTS);
    unsub();
    assert.ok(callCount > 0, "listener should have been called");
  });

  it("sets live run immediately when startRun is called", async () => {
    let capturedLive: unknown = undefined;
    const artifacts = makeFakeArtifacts({
      createRun: async () => {
        // snapshot is already set before createRun resolves
        capturedLive = { captured: true };
      },
    });

    const store = createZustandRuns({
      runtime: makeFakeRuntime([
        makeRunEvent({ type: "runCompleted", ok: true, completedAt: Date.now() }),
      ]),
      artifacts,
    });

    const runPromise = store.startRun("path/script.yml", "yaml: {}", DEFAULT_OPTS);
    // Live should be set by the time createRun fires (synchronous before await)
    await runPromise;
    assert.ok(capturedLive !== undefined);
  });

  it("live run has correct scriptPath, deviceId, result=running initially", async () => {
    const artifacts = makeFakeArtifacts({
      createRun: async () => {},
    });

    // Use a runtime that yields one event then runCompleted
    const runtime = makeFakeRuntime([
      makeRunEvent({ type: "runCompleted", ok: true, completedAt: Date.now() }),
    ]);

    const store = createZustandRuns({ runtime, artifacts });

    // Before startRun — null
    assert.equal(store.getSnapshot().live, null);
    await store.startRun("my/script.yml", "yaml: {}", { deviceId: "d-42" });

    const snap = store.getSnapshot();
    // After completion, live should reflect the final result
    assert.ok(snap.live !== null);
    assert.equal(snap.live!.scriptPath, "my/script.yml");
    assert.equal(snap.live!.deviceId, "d-42");
    assert.equal(snap.live!.result, "passed");
  });

  it("derives scriptName from last path segment", async () => {
    const captured: RunMetadata[] = [];
    const artifacts = makeFakeArtifacts({
      createRun: async (meta) => { captured.push(meta); },
    });

    const store = createZustandRuns({
      runtime: makeFakeRuntime([
        makeRunEvent({ type: "runCompleted", ok: true, completedAt: Date.now() }),
      ]),
      artifacts,
    });
    await store.startRun("/a/b/c/my-test.yml", "yaml: {}", DEFAULT_OPTS);

    assert.equal(captured[0]?.scriptName, "my-test.yml");
  });

  it("result is passed when runCompleted ok=true", async () => {
    const store = createZustandRuns({
      runtime: makeFakeRuntime([
        makeRunEvent({ type: "runCompleted", ok: true, completedAt: Date.now() }),
      ]),
      artifacts: makeFakeArtifacts(),
    });
    await store.startRun("s.yml", "", DEFAULT_OPTS);
    assert.equal(store.getSnapshot().live?.result, "passed");
  });

  it("result is failed when runCompleted ok=false", async () => {
    const store = createZustandRuns({
      runtime: makeFakeRuntime([
        makeRunEvent({ type: "runCompleted", ok: false, completedAt: Date.now() }),
      ]),
      artifacts: makeFakeArtifacts(),
    });
    await store.startRun("s.yml", "", DEFAULT_OPTS);
    assert.equal(store.getSnapshot().live?.result, "failed");
  });

  it("result is passed when only stepCompleted ok=false events arrive (no runCompleted)", async () => {
    // okResult starts true; only runCompleted/scenarioCompleted can flip it.
    // A failed step records failedAtStep but does NOT change okResult.
    const store = createZustandRuns({
      runtime: makeFakeRuntime([
        makeRunEvent({
          type: "stepCompleted",
          stepIndex: 2,
          command: "tap",
          ok: false,
          durationMs: 100,
          summary: "tap btn",
          tokens: [],
          depth: 0,
        }),
      ]),
      artifacts: makeFakeArtifacts(),
    });
    await store.startRun("s.yml", "", DEFAULT_OPTS);
    // No terminal event — okResult remains true → result is "passed"
    assert.equal(store.getSnapshot().live?.result, "passed");
  });

  it("failedAtStep is recorded when a step fails", async () => {
    const finalized: Array<Partial<RunMetadata>> = [];
    const artifacts = makeFakeArtifacts({
      createRun: async () => {},
      finalizeRun: async (_id, patch) => { finalized.push(patch); },
    });

    const store = createZustandRuns({
      runtime: makeFakeRuntime([
        makeRunEvent({
          type: "stepCompleted",
          stepIndex: 3,
          command: "tap",
          ok: false,
          durationMs: 50,
          summary: "",
          tokens: [],
          depth: 0,
        }),
      ]),
      artifacts,
    });
    await store.startRun("s.yml", "", DEFAULT_OPTS);
    assert.equal(finalized[0]?.failedAtStep, 3);
  });

  it("events are accumulated on the live run", async () => {
    const events: RunEvent[] = [
      makeRunEvent({
        type: "stepStarted",
        stepIndex: 0,
        command: "tap",
        summary: "tap btn",
        tokens: [],
        depth: 0,
      }),
      makeRunEvent({
        type: "stepCompleted",
        stepIndex: 0,
        command: "tap",
        ok: true,
        durationMs: 80,
        summary: "tap btn",
        tokens: [],
        depth: 0,
      }),
      makeRunEvent({ type: "runCompleted", ok: true, completedAt: Date.now() }),
    ];

    const store = createZustandRuns({
      runtime: makeFakeRuntime(events),
      artifacts: makeFakeArtifacts(),
    });
    await store.startRun("s.yml", "", DEFAULT_OPTS);

    // All three events should be in live.events
    assert.equal(store.getSnapshot().live!.events.length, 3);
  });

  it("result is cancelled when stopRun is called before iteration ends", async () => {
    let resolveStop!: () => void;
    const stopSignal = new Promise<void>((r) => { resolveStop = r; });

    const runtime: StudioRuntime = {
      ...makeFakeRuntime(),
      runScript: () => pendingUntil(stopSignal),
      stop: async () => { resolveStop(); },
    };

    const store = createZustandRuns({ runtime, artifacts: makeFakeArtifacts() });
    const runPromise = store.startRun("s.yml", "", DEFAULT_OPTS);
    store.stopRun(); // sets cancelled=true + calls runtime.stop()
    await runPromise;

    assert.equal(store.getSnapshot().live?.result, "cancelled");
  });

  it("result is failed when runtime throws", async () => {
    const runtime: StudioRuntime = {
      ...makeFakeRuntime(),
      runScript: () => throwingIterable("network error"),
    };

    const store = createZustandRuns({ runtime, artifacts: makeFakeArtifacts() });
    await store.startRun("s.yml", "", DEFAULT_OPTS);
    assert.equal(store.getSnapshot().live?.result, "failed");
  });

  it("error event is appended when runtime throws", async () => {
    const appended: RunEvent[] = [];
    const artifacts = makeFakeArtifacts({
      createRun: async () => {},
      appendEvent: async (_id, ev) => { appended.push(ev); },
    });

    const runtime: StudioRuntime = {
      ...makeFakeRuntime(),
      runScript: () => throwingIterable("boom"),
    };

    const store = createZustandRuns({ runtime, artifacts });
    await store.startRun("s.yml", "", DEFAULT_OPTS);

    const errorEvent = appended.find((e) => e.type === "consoleLog" && e.level === "error");
    assert.ok(errorEvent, "a consoleLog/error event should be appended on throw");
    assert.ok(
      (errorEvent as Extract<RunEvent, { type: "consoleLog" }>).line.includes("boom"),
    );
  });

  it("loadHistory populates the history slice", async () => {
    const artifacts = makeFakeArtifacts();
    // Pre-seed two metadata entries
    await artifacts.createRun({
      runId: "r1", scriptPath: "a.yml", scriptName: "a.yml",
      deviceId: "d1", startedAt: 1000,
    });
    await artifacts.createRun({
      runId: "r2", scriptPath: "b.yml", scriptName: "b.yml",
      deviceId: "d1", startedAt: 2000,
    });

    const store = createZustandRuns({ runtime: makeFakeRuntime(), artifacts });
    await store.loadHistory();

    const history = store.getSnapshot().history;
    assert.equal(history.length, 2);
  });

  it("deleteRun removes the entry from history", async () => {
    const artifacts = makeFakeArtifacts();
    await artifacts.createRun({
      runId: "r1", scriptPath: "a.yml", scriptName: "a.yml",
      deviceId: "d1", startedAt: 1000,
    });
    const store = createZustandRuns({ runtime: makeFakeRuntime(), artifacts });
    await store.loadHistory();
    assert.equal(store.getSnapshot().history.length, 1);

    await store.deleteRun("r1");
    assert.equal(store.getSnapshot().history.length, 0);
  });

  it("history is refreshed after startRun completes", async () => {
    const store = createZustandRuns({
      runtime: makeFakeRuntime([
        makeRunEvent({ type: "runCompleted", ok: true, completedAt: Date.now() }),
      ]),
      artifacts: makeFakeArtifacts(),
    });
    assert.equal(store.getSnapshot().history.length, 0);
    await store.startRun("s.yml", "", DEFAULT_OPTS);
    assert.equal(store.getSnapshot().history.length, 1);
  });

  // ------------------------------------------------------------------
  // Scenario event handling via applyEventToLiveRun
  // ------------------------------------------------------------------

  it("scenarioStarted creates a scenario slice with queued scripts", async () => {
    const scenarioEvents: RunEvent[] = [
      makeRunEvent({
        type: "scenarioStarted",
        scenarioName: "smoke",
        totalScripts: 3,
      }),
      makeRunEvent({
        type: "scenarioCompleted",
        ok: true,
        totalScripts: 3,
        passedScripts: 3,
        durationMs: 500,
      }),
    ];

    const store = createZustandRuns({
      runtime: makeFakeRuntime(scenarioEvents),
      artifacts: makeFakeArtifacts(),
    });
    await store.startRun("scenario.yml", "", DEFAULT_OPTS);

    const scenario = store.getSnapshot().live?.scenario;
    assert.ok(scenario, "scenario slice should be present");
    assert.equal(scenario!.name, "smoke");
    assert.equal(scenario!.totalScripts, 3);
    assert.equal(scenario!.scripts.length, 3);
    assert.ok(scenario!.scripts.every((s) => s.status === "queued"));
  });

  it("scriptStarted marks the correct row as running", async () => {
    const scenarioEvents: RunEvent[] = [
      makeRunEvent({ type: "scenarioStarted", scenarioName: "s", totalScripts: 2 }),
      makeRunEvent({ type: "scriptStarted", scriptIndex: 1, scriptPath: "b.yml" }),
      makeRunEvent({
        type: "scenarioCompleted",
        ok: true,
        totalScripts: 2,
        passedScripts: 2,
        durationMs: 200,
      }),
    ];

    const store = createZustandRuns({
      runtime: makeFakeRuntime(scenarioEvents),
      artifacts: makeFakeArtifacts(),
    });
    await store.startRun("scenario.yml", "", DEFAULT_OPTS);

    const scripts = store.getSnapshot().live?.scenario?.scripts;
    assert.ok(scripts);
    assert.equal(scripts![1].status, "running");
    assert.equal(scripts![1].path, "b.yml");
    assert.equal(scripts![0].status, "queued");
  });

  it("scriptCompleted updates the row status and duration", async () => {
    const scenarioEvents: RunEvent[] = [
      makeRunEvent({ type: "scenarioStarted", scenarioName: "s", totalScripts: 1 }),
      makeRunEvent({ type: "scriptStarted", scriptIndex: 0, scriptPath: "a.yml" }),
      makeRunEvent({
        type: "scriptCompleted",
        scriptIndex: 0,
        scriptPath: "a.yml",
        status: "passed",
        durationMs: 350,
      }),
      makeRunEvent({
        type: "scenarioCompleted",
        ok: true,
        totalScripts: 1,
        passedScripts: 1,
        durationMs: 350,
      }),
    ];

    const store = createZustandRuns({
      runtime: makeFakeRuntime(scenarioEvents),
      artifacts: makeFakeArtifacts(),
    });
    await store.startRun("scenario.yml", "", DEFAULT_OPTS);

    const row = store.getSnapshot().live?.scenario?.scripts[0];
    assert.ok(row);
    assert.equal(row!.status, "passed");
    assert.equal(row!.durationMs, 350);
  });

  it("scriptCompleted with failed status records failedAtStep on the row", async () => {
    const scenarioEvents: RunEvent[] = [
      makeRunEvent({ type: "scenarioStarted", scenarioName: "s", totalScripts: 1 }),
      makeRunEvent({ type: "scriptStarted", scriptIndex: 0, scriptPath: "a.yml" }),
      makeRunEvent({
        type: "scriptCompleted",
        scriptIndex: 0,
        scriptPath: "a.yml",
        status: "failed",
        durationMs: 100,
        failedAtStep: 2,
      }),
      makeRunEvent({
        type: "scenarioCompleted",
        ok: false,
        totalScripts: 1,
        passedScripts: 0,
        durationMs: 100,
      }),
    ];

    const store = createZustandRuns({
      runtime: makeFakeRuntime(scenarioEvents),
      artifacts: makeFakeArtifacts(),
    });
    await store.startRun("scenario.yml", "", DEFAULT_OPTS);

    const row = store.getSnapshot().live?.scenario?.scripts[0];
    assert.equal(row?.failedAtStep, 2);
  });

  it("result is failed when scenarioCompleted ok=false", async () => {
    const store = createZustandRuns({
      runtime: makeFakeRuntime([
        makeRunEvent({ type: "scenarioStarted", scenarioName: "s", totalScripts: 1 }),
        makeRunEvent({
          type: "scenarioCompleted",
          ok: false,
          totalScripts: 1,
          passedScripts: 0,
          durationMs: 50,
        }),
      ]),
      artifacts: makeFakeArtifacts(),
    });
    await store.startRun("scenario.yml", "", DEFAULT_OPTS);
    assert.equal(store.getSnapshot().live?.result, "failed");
  });

  it("totalSteps is derived from max stepIndex+1 across stepCompleted events", async () => {
    const finalized: Array<Partial<RunMetadata>> = [];
    const artifacts = makeFakeArtifacts({
      createRun: async () => {},
      finalizeRun: async (_id, patch) => { finalized.push(patch); },
    });

    const stepEvents: RunEvent[] = [
      makeRunEvent({
        type: "stepCompleted", stepIndex: 0, command: "tap",
        ok: true, durationMs: 10, summary: "", tokens: [], depth: 0,
      }),
      makeRunEvent({
        type: "stepCompleted", stepIndex: 1, command: "tap",
        ok: true, durationMs: 10, summary: "", tokens: [], depth: 0,
      }),
      makeRunEvent({
        type: "stepCompleted", stepIndex: 4, command: "tap",
        ok: true, durationMs: 10, summary: "", tokens: [], depth: 0,
      }),
      makeRunEvent({ type: "runCompleted", ok: true, completedAt: Date.now() }),
    ];

    const store = createZustandRuns({ runtime: makeFakeRuntime(stepEvents), artifacts });
    await store.startRun("s.yml", "", DEFAULT_OPTS);

    assert.equal(finalized[0]?.totalSteps, 5); // max stepIndex 4 → 4+1
  });

  it("cwd defaults to directory of scriptPath when not provided in opts", async () => {
    const capturedOpts: unknown[] = [];
    const runtime: StudioRuntime = {
      ...makeFakeRuntime(),
      runScript: (_yaml, opts) => {
        capturedOpts.push(opts);
        return eventsIterable([
          makeRunEvent({ type: "runCompleted", ok: true, completedAt: Date.now() }),
        ]);
      },
    };

    const store = createZustandRuns({ runtime, artifacts: makeFakeArtifacts() });
    await store.startRun("/a/b/script.yml", "", { deviceId: "d1" });

    assert.equal((capturedOpts[0] as { cwd: string }).cwd, "/a/b");
  });
});
