import { test } from "node:test";
import { strict as assert } from "node:assert";
import { MockRuntime } from "../../../domain/features/runtime/index.js";
import type { UiTreeNode } from "../../../domain/features/runtime/index.js";
import { computeTreeExtent } from "./index.js";
import { createZustandUiInspector } from "./ui-inspector.zustand.js";

const SAMPLE_TREE: UiTreeNode = {
  attributes: { class: "FrameLayout" },
  children: [
    {
      attributes: { class: "Button", text: "Login" },
      children: [],
      clickable: true,
    },
  ],
};

async function connectedMock(tree: UiTreeNode | undefined = SAMPLE_TREE) {
  const runtime = new MockRuntime({
    uiTreesByDevice: tree ? { "dev-1": tree } : undefined,
  });
  await runtime.connect();
  return runtime;
}

test("ui-inspector feature", async (t) => {
  await t.test("starts empty", () => {
    const inspector = createZustandUiInspector({
      runtime: new MockRuntime(),
    });
    const snap = inspector.getSnapshot();
    assert.equal(snap.tree, null);
    assert.equal(snap.selectedPath, null);
    assert.equal(snap.loading, false);
    assert.equal(snap.capturedForDeviceId, null);
  });

  await t.test("refresh loads tree and records device id", async () => {
    const runtime = await connectedMock();
    const inspector = createZustandUiInspector({ runtime });
    await inspector.refresh("dev-1");
    const snap = inspector.getSnapshot();
    assert.equal(snap.tree, SAMPLE_TREE);
    assert.equal(snap.capturedForDeviceId, "dev-1");
    assert.equal(snap.loading, false);
    assert.equal(snap.error, null);
  });

  await t.test("refresh captures error on failure", async () => {
    const runtime = new MockRuntime();
    // MockRuntime without connect() throws — the failure surfaces
    // through refresh and leaves the snapshot in error state.
    const inspector = createZustandUiInspector({ runtime });
    await inspector.refresh("dev-1");
    const snap = inspector.getSnapshot();
    assert.equal(snap.tree, null);
    assert.equal(snap.loading, false);
    assert.ok(snap.error && snap.error.length > 0);
  });

  await t.test("select + clear update snapshot", async () => {
    const runtime = await connectedMock();
    const inspector = createZustandUiInspector({ runtime });
    await inspector.refresh("dev-1");
    inspector.select("0");
    assert.equal(inspector.getSnapshot().selectedPath, "0");
    inspector.clear();
    const snap = inspector.getSnapshot();
    assert.equal(snap.tree, null);
    assert.equal(snap.selectedPath, null);
    assert.equal(snap.capturedForDeviceId, null);
    inspector.dispose();
  });

  await t.test("auto-refresh defaults: disabled, 5000ms", async () => {
    const runtime = await connectedMock();
    const inspector = createZustandUiInspector({ runtime });
    const snap = inspector.getSnapshot();
    assert.equal(snap.autoRefreshEnabled, false);
    assert.equal(snap.autoRefreshIntervalMs, 5000);
    assert.equal(snap.autoRefreshPaused, false);
    inspector.dispose();
  });

  await t.test("setAutoRefreshInterval clamps below 2000ms", () => {
    const inspector = createZustandUiInspector({
      runtime: new MockRuntime(),
    });
    inspector.setAutoRefreshInterval(500);
    assert.equal(inspector.getSnapshot().autoRefreshIntervalMs, 2000);
    inspector.setAutoRefreshInterval(7500);
    assert.equal(inspector.getSnapshot().autoRefreshIntervalMs, 7500);
    inspector.setAutoRefreshInterval(Number.NaN);
    assert.equal(inspector.getSnapshot().autoRefreshIntervalMs, 5000);
    inspector.dispose();
  });

  await t.test("auto-refresh tick re-fetches captured device", async (tt) => {
    tt.mock.timers.enable({ apis: ["setInterval"] });
    let calls = 0;
    const runtime = await connectedMock();
    const original = runtime.getUiTree.bind(runtime);
    runtime.getUiTree = async (id: string) => {
      calls++;
      return original(id);
    };
    const inspector = createZustandUiInspector({
      runtime,
      autoRefresh: { enabled: false, intervalMs: 2000 },
    });
    await inspector.refresh("dev-1");
    assert.equal(calls, 1);
    inspector.setAutoRefreshEnabled(true);
    tt.mock.timers.tick(2000);
    await Promise.resolve();
    await Promise.resolve();
    assert.ok(calls >= 2, `expected ≥2 getUiTree calls, got ${calls}`);
    inspector.dispose();
  });

  await t.test("auto-refresh pauses for 1s after interaction", async (tt) => {
    tt.mock.timers.enable({ apis: ["setInterval", "Date"] });
    let calls = 0;
    const runtime = await connectedMock();
    const original = runtime.getUiTree.bind(runtime);
    runtime.getUiTree = async (id: string) => {
      calls++;
      return original(id);
    };
    const inspector = createZustandUiInspector({
      runtime,
      autoRefresh: { enabled: true, intervalMs: 2000 },
    });
    await inspector.refresh("dev-1");
    const baseline = calls;
    tt.mock.timers.tick(1500);
    inspector.notifyInteraction();
    tt.mock.timers.tick(500);
    await Promise.resolve();
    assert.equal(calls, baseline, "tick within pause window must not refresh");
    assert.equal(inspector.getSnapshot().autoRefreshPaused, true);
    tt.mock.timers.tick(2000);
    await Promise.resolve();
    await Promise.resolve();
    assert.ok(calls > baseline, "tick after pause window must refresh");
    assert.equal(inspector.getSnapshot().autoRefreshPaused, false);
    inspector.dispose();
  });

  await t.test("disable stops the timer and clears paused flag", async (tt) => {
    tt.mock.timers.enable({ apis: ["setInterval"] });
    let calls = 0;
    const runtime = await connectedMock();
    const original = runtime.getUiTree.bind(runtime);
    runtime.getUiTree = async (id: string) => {
      calls++;
      return original(id);
    };
    const inspector = createZustandUiInspector({
      runtime,
      autoRefresh: { enabled: true, intervalMs: 2000 },
    });
    await inspector.refresh("dev-1");
    const baseline = calls;
    inspector.setAutoRefreshEnabled(false);
    tt.mock.timers.tick(10000);
    await Promise.resolve();
    assert.equal(calls, baseline);
    assert.equal(inspector.getSnapshot().autoRefreshEnabled, false);
    assert.equal(inspector.getSnapshot().autoRefreshPaused, false);
    inspector.dispose();
  });
});

test("computeTreeExtent", async (t) => {
  await t.test("returns null for missing tree", () => {
    assert.equal(computeTreeExtent(null), null);
  });

  await t.test("prefers root bounds when present", () => {
    const tree: UiTreeNode = {
      attributes: { bounds: "0,0,1080,2340" },
      children: [
        {
          attributes: { bounds: "0,0,100,100" },
          children: [],
        },
      ],
    };
    assert.deepEqual(computeTreeExtent(tree), {
      width: 1080,
      height: 2340,
    });
  });

  await t.test("falls back to max right/bottom when root has no bounds", () => {
    const tree: UiTreeNode = {
      attributes: {},
      children: [
        {
          attributes: { bounds: "10,20,500,600" },
          children: [],
        },
        {
          attributes: { bounds: "0,0,1080,2340" },
          children: [
            {
              attributes: { bounds: "100,200,1100,2400" },
              children: [],
            },
          ],
        },
      ],
    };
    assert.deepEqual(computeTreeExtent(tree), {
      width: 1100,
      height: 2400,
    });
  });

  await t.test("returns null when no node carries parseable bounds", () => {
    const tree: UiTreeNode = {
      attributes: {},
      children: [{ attributes: {}, children: [] }],
    };
    assert.equal(computeTreeExtent(tree), null);
  });
});
