import { describe, it } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import type { AddressInfo } from "node:net";
import { AndroidDriver } from "./android.driver.js";

/**
 * Unit tests for `AndroidDriver` running against a local fake
 * HTTP server. Each test spins up a `http.createServer()` bound
 * to an ephemeral port and points the driver at it — that
 * bypasses `adb forward` entirely (the forward would fail on a
 * machine without a connected device, and is not what we want
 * to cover here anyway).
 *
 * `connect()` does call `adbForward` internally, so we monkey-
 * patch it via subclassing. A cleaner approach would be to
 * inject the forward function, but keeping the Driver
 * constructor API small is worth the minor test friction.
 */

class TestAndroidDriver extends AndroidDriver {
  async connect(): Promise<void> {
    // Skip adb forward; rely on the caller to point `hostPort`
    // at an already-listening server. Mirror production `connect()`
    // sequence: /health liveness → /ping capability handshake.
    const self = this as unknown as {
      http: { get: (p: string) => Promise<unknown> };
      applyPingCapabilities: (p: Record<string, unknown>) => void;
      _gestureMechanism: string | null;
      connected: boolean;
    };
    await self.http.get("/health");
    try {
      const pong = (await self.http.get("/ping")) as Record<string, unknown>;
      self.applyPingCapabilities(pong);
    } catch {
      self._gestureMechanism = null;
    }
    self.connected = true;
  }
  async reconnect(): Promise<void> {
    // Mirror the real reconnect minus adb forward: re-issue /health
    // + /ping and surface an actionable error on ping failure so
    // stale-binding detection stays covered by tests.
    const self = this as unknown as {
      http: { get: (p: string) => Promise<unknown> };
      applyPingCapabilities: (p: Record<string, unknown>) => void;
      connected: boolean;
    };
    await self.http.get("/health");
    try {
      const pong = (await self.http.get("/ping")) as Record<string, unknown>;
      self.applyPingCapabilities(pong);
    } catch (err) {
      throw new Error(
        `reconnect /ping failed: ${(err as Error).message}`,
      );
    }
    self.connected = true;
  }
  async disconnect(): Promise<void> {
    (this as unknown as { connected: boolean }).connected = false;
  }
}

interface FakeCall {
  method: string;
  path: string;
  body?: unknown;
}

function startFakeServer(
  handler: (call: FakeCall, res: http.ServerResponse) => void,
): Promise<{ port: number; close: () => Promise<void>; calls: FakeCall[] }> {
  return new Promise((resolve) => {
    const calls: FakeCall[] = [];
    const server = http.createServer((req, res) => {
      let bodyBuf = "";
      req.on("data", (chunk) => (bodyBuf += chunk));
      req.on("end", () => {
        const body = bodyBuf ? JSON.parse(bodyBuf) : undefined;
        const call: FakeCall = {
          method: req.method ?? "",
          path: req.url ?? "",
          body,
        };
        calls.push(call);
        handler(call, res);
      });
    });
    server.listen(0, "127.0.0.1", () => {
      const address = server.address() as AddressInfo;
      resolve({
        port: address.port,
        calls,
        close: () => new Promise((r) => server.close(() => r())),
      });
    });
  });
}

/**
 * Spin up a fresh fake server + driver for each test. Used
 * inline in `it()` blocks rather than via beforeEach hooks
 * because node:test's hook semantics around async cleanup are
 * unreliable when a test spawns network resources.
 */
async function setup(
  opts: {
    /**
     * Override the `/ping` response — defaults to a pong that
     * reports no multi-pointer, no pressure, and
     * `mechanism: "accessibility"`. Tests that exercise capability
     * propagation supply a different shape here.
     */
    pingResponse?: Record<string, unknown> | null;
  } = {},
): Promise<{
  server: Awaited<ReturnType<typeof startFakeServer>>;
  driver: TestAndroidDriver;
  /**
   * Swap the active `/ping` response mid-test. Used by reconnect
   * tests to simulate capability drift: set the new pong, call
   * `driver.reconnect()`, assert new flags propagated.
   */
  setPingResponse: (next: Record<string, unknown> | null) => void;
  cleanup: () => Promise<void>;
}> {
  const pingRef: { current: Record<string, unknown> | null } = {
    current:
      opts.pingResponse === null
        ? null
        : (opts.pingResponse ?? {
            ok: true,
            agent: "atomyx-android",
            mechanism: "accessibility",
            capabilities: { canMultiPointer: false, canPressure: false },
          }),
  };
  const server = await startFakeServer((call, res) => {
    res.setHeader("content-type", "application/json");
    if (call.path === "/health") {
      res.end(JSON.stringify({ ok: true, accessibilityConnected: true }));
      return;
    }
    if (call.path === "/ping") {
      if (pingRef.current === null) {
        res.statusCode = 404;
        res.end(JSON.stringify({ ok: false, error: "not found" }));
        return;
      }
      res.end(JSON.stringify(pingRef.current));
      return;
    }
    if (call.path === "/tree") {
      // Matches the real `UiTreeService.dumpTree` shape: synthetic
      // `el_root` wrapper has NO bounds (it's just a container),
      // the real window root is the first child with its own bounds.
      // A previous test fake lied by injecting bounds on the root,
      // which hid a production bug where `screenSize()` threw
      // "root has no bounds attribute" on real devices.
      res.end(
        JSON.stringify({
          elementId: "el_root",
          className: "Root",
          children: [
            {
              elementId: "window_root",
              className: "android.widget.FrameLayout",
              bounds: { left: 0, top: 0, right: 1080, bottom: 2400 },
              children: [
                {
                  elementId: "btn",
                  className: "android.widget.Button",
                  resourceId: "com.app:id/login",
                  text: "Sign in",
                  bounds: { left: 100, top: 500, right: 900, bottom: 620 },
                  clickable: true,
                  enabled: true,
                },
              ],
            },
          ],
        }),
      );
      return;
    }
    if (call.path === "/screenshot") {
      res.end(JSON.stringify({ base64: Buffer.from("hi").toString("base64"), format: "png" }));
      return;
    }
    if (call.path === "/apps") {
      res.end(JSON.stringify([{ packageName: "com.app", label: "App" }]));
      return;
    }
    if (call.path === "/current-activity") {
      res.end(JSON.stringify({ packageName: "com.app", activity: ".MainActivity" }));
      return;
    }
    res.end(JSON.stringify({ ok: true }));
  });
  const driver = new TestAndroidDriver({ serial: "test-device", hostPort: server.port });
  await driver.connect();
  return {
    server,
    driver,
    setPingResponse: (next) => {
      pingRef.current = next;
    },
    cleanup: async () => {
      await driver.disconnect();
      await server.close();
    },
  };
}


describe("AndroidDriver.hierarchy", () => {
  it("returns a canonical TreeNode with normalized attributes", async () => {
    const { driver, cleanup } = await setup();
    try {
      const tree = await driver.hierarchy();
      // The synthetic el_root wrapper has className="Root" which
      // the classNameToRole heuristic maps to "other" (it's not a
      // FrameLayout/LinearLayout/etc). The window_root child IS the
      // container.
      assert.equal(tree.attributes["role"], "other");
      assert.equal(tree.children.length, 1);
      const windowRoot = tree.children[0]!;
      assert.equal(windowRoot.attributes["role"], "container");
      assert.equal(windowRoot.children.length, 1);
      const btn = windowRoot.children[0]!;
      assert.equal(btn.attributes["id"], "com.app:id/login");
      assert.equal(btn.attributes["text"], "Sign in");
      assert.equal(btn.attributes["role"], "button");
      assert.equal(btn.attributes["bounds"], "100,500,900,620");
      assert.equal(btn.clickable, true);
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.tap", () => {
  it("posts to /actions/tap_coords with x/y body", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      await driver.tap({ x: 500, y: 800 });
      const tap = server.calls.find((c) => c.path === "/actions/tap_coords");
      assert.ok(tap);
      assert.deepEqual(tap!.body, { x: 500, y: 800 });
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.swipe", () => {
  it("posts to /actions/swipe with from/to/durationMs flattened", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      await driver.swipe({ x: 100, y: 200 }, { x: 300, y: 400 }, 250);
      const call = server.calls.find((c) => c.path === "/actions/swipe");
      assert.ok(call);
      assert.deepEqual(call!.body, {
        fromX: 100,
        fromY: 200,
        toX: 300,
        toY: 400,
        durationMs: 250,
      });
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.longPress", () => {
  it("posts to /actions/long_press with x/y/durationMs", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      await driver.longPress({ x: 50, y: 60 }, 1000);
      const call = server.calls.find((c) => c.path === "/actions/long_press");
      assert.deepEqual(call!.body, { x: 50, y: 60, durationMs: 1000 });
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.inputText + eraseText + pressKey", () => {
  it("posts inputText to /actions/type_keyboard with clearFirst:false", async () => {
    // Regression: the APK's `/actions/type_keyboard` route defaults
    // `clearFirst` to true when the caller omits the field, which
    // used to silently override Orchestra's "append" contract.
    // The adapter now explicitly sends `clearFirst: false` so the
    // APK never double-clears — Orchestra drives clearing via
    // `eraseText` when it wants to clear.
    const { driver, server, cleanup } = await setup();
    try {
      await driver.inputText("hello");
      const call = server.calls.find((c) => c.path === "/actions/type_keyboard");
      assert.deepEqual(call!.body, { text: "hello", clearFirst: false });
    } finally {
      await cleanup();
    }
  });

  it("eraseText uses /actions/clear_focused_input, one RPC regardless of count", async () => {
    // Regression: previous implementation looped N times posting
    // {key: "delete"} to /actions/key, which on a default
    // Orchestra call (count=999) meant 999 serial HTTP roundtrips.
    // The APK has a native bulk-clear route — use it once.
    const { driver, server, cleanup } = await setup();
    try {
      await driver.eraseText(999);
      const clears = server.calls.filter(
        (c) => c.path === "/actions/clear_focused_input",
      );
      assert.equal(clears.length, 1);
      // And we did NOT fan out into individual delete key presses.
      const deletes = server.calls.filter(
        (c) =>
          c.path === "/actions/key" &&
          (c.body as { key?: string } | undefined)?.key === "delete",
      );
      assert.equal(deletes.length, 0);
    } finally {
      await cleanup();
    }
  });

  it("posts pressKey to /actions/key and always reports ok", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      const result = await driver.pressKey("back");
      assert.equal(result.ok, true);
      const call = server.calls.find((c) => c.path === "/actions/key");
      assert.deepEqual(call!.body, { key: "back" });
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.launchApp / stopApp / killApp", () => {
  it("launch uses packageName field", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      await driver.launchApp("com.example.app");
      const call = server.calls.find((c) => c.path === "/actions/launch");
      assert.deepEqual(call!.body, { packageName: "com.example.app" });
    } finally {
      await cleanup();
    }
  });

  it("stop + kill both use /actions/force_stop", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      await driver.stopApp("com.a");
      await driver.killApp("com.b");
      const stops = server.calls.filter((c) => c.path === "/actions/force_stop");
      assert.equal(stops.length, 2);
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.currentForeground + listApps", () => {
  it("maps packageName → bundleId", async () => {
    const { driver, cleanup } = await setup();
    try {
      const fg = await driver.currentForeground();
      assert.equal(fg.bundleId, "com.app");
      assert.equal(fg.activity, ".MainActivity");
    } finally {
      await cleanup();
    }
  });

  it("listApps maps packageName → bundleId and label → displayName", async () => {
    const { driver, cleanup } = await setup();
    try {
      const apps = await driver.listApps();
      assert.equal(apps.length, 1);
      assert.equal(apps[0]!.bundleId, "com.app");
      assert.equal(apps[0]!.displayName, "App");
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.screenshot", () => {
  it("decodes base64 into a Uint8Array", async () => {
    const { driver, cleanup } = await setup();
    try {
      const bytes = await driver.screenshot();
      assert.ok(bytes.length > 0);
      assert.equal(Buffer.from(bytes).toString(), "hi");
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.screenSize", () => {
  it("derives width + height from root bounds", async () => {
    const { driver, cleanup } = await setup();
    try {
      const size = await driver.screenSize();
      assert.equal(size.width, 1080);
      assert.equal(size.height, 2400);
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.capabilities", () => {
  it("reports canEraseText=true (backed by /actions/clear_focused_input)", async () => {
    const { driver, cleanup } = await setup();
    try {
      const caps = driver.capabilities;
      assert.equal(caps.canEraseText, true);
      assert.equal(caps.canWaitForIdle, false);
      assert.equal(caps.canScreenshot, true);
      assert.equal(caps.canHideKeyboard, true);
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver ping handshake", () => {
  it("populates capabilities from /ping response", async () => {
    const { driver, cleanup } = await setup({
      pingResponse: {
        ok: true,
        agent: "atomyx-android",
        mechanism: "accessibility-multistroke",
        capabilities: { canMultiPointer: true, canPressure: false },
      },
    });
    try {
      assert.equal(driver.capabilities.canMultiPointer, true);
      assert.equal(driver.capabilities.canPressure, false);
      assert.equal(driver.gestureMechanism, "accessibility-multistroke");
    } finally {
      await cleanup();
    }
  });

  it("keeps conservative defaults when /ping returns 404 (old agent)", async () => {
    // Regression: contract requires that a pre-capability APK
    // still connects cleanly. A silent fallback to
    // canMultiPointer=false preserves existing behaviour; a
    // thrown ping would break connect() for users not on the
    // latest agent build.
    const { driver, cleanup } = await setup({ pingResponse: null });
    try {
      assert.equal(driver.capabilities.canMultiPointer, false);
      assert.equal(driver.capabilities.canPressure, false);
      assert.equal(driver.gestureMechanism, null);
    } finally {
      await cleanup();
    }
  });

  it("treats non-boolean capability fields as false", async () => {
    // Defence-in-depth: the APK might send a malformed value for
    // canMultiPointer (string "true", number 1, null). The host
    // MUST coerce to strict boolean and reject anything else,
    // because canMultiPointer gates whether multi-pointer YAML
    // scripts dispatch. Loose equality here would leak unsafe
    // gestures into the driver.
    const { driver, cleanup } = await setup({
      pingResponse: {
        ok: true,
        mechanism: "accessibility",
        capabilities: { canMultiPointer: "true", canPressure: 1 },
      },
    });
    try {
      assert.equal(driver.capabilities.canMultiPointer, false);
      assert.equal(driver.capabilities.canPressure, false);
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.reconnect", () => {
  it("re-runs /health + /ping handshake", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      const healthBefore = server.calls.filter((c) => c.path === "/health").length;
      const pingBefore = server.calls.filter((c) => c.path === "/ping").length;
      await driver.reconnect();
      const healthAfter = server.calls.filter((c) => c.path === "/health").length;
      const pingAfter = server.calls.filter((c) => c.path === "/ping").length;
      assert.equal(healthAfter, healthBefore + 1);
      assert.equal(pingAfter, pingBefore + 1);
      assert.equal(driver.isConnected(), true);
    } finally {
      await cleanup();
    }
  });

  it("picks up capability drift across reconnect", async () => {
    // Regression: if the APK upgrades mid-session from a
    // single-pointer build to a multi-pointer build, a reconnect
    // must refresh capabilities. Otherwise the host keeps
    // rejecting gestures the APK can now dispatch.
    const { driver, setPingResponse, cleanup } = await setup({
      pingResponse: {
        ok: true,
        mechanism: "accessibility",
        capabilities: { canMultiPointer: false, canPressure: false },
      },
    });
    try {
      assert.equal(driver.capabilities.canMultiPointer, false);
      setPingResponse({
        ok: true,
        mechanism: "accessibility-multistroke",
        capabilities: { canMultiPointer: true, canPressure: false },
      });
      await driver.reconnect();
      assert.equal(driver.capabilities.canMultiPointer, true);
      assert.equal(driver.gestureMechanism, "accessibility-multistroke");
    } finally {
      await cleanup();
    }
  });

  it("throws when /ping fails — stale-binding detection", async () => {
    // Contract: /health passing alone is not enough. A bystander
    // process on the same port, or a regressed APK build without
    // /ping, would let reconnect silently succeed and dispatch
    // commands to the wrong target. The caller must get an
    // actionable error instead.
    const { driver, setPingResponse, cleanup } = await setup();
    try {
      setPingResponse(null); // ping now 404s
      await assert.rejects(
        () => driver.reconnect(),
        /ping failed/,
      );
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.dispatchGesture", () => {
  it("single-pointer tap posts to /actions/dispatch_gesture with canonical waypoints", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      await driver.dispatchGesture({
        pointers: [
          {
            id: "finger1",
            waypoints: [
              { phase: "down", point: { x: 100, y: 200 }, atOffsetSeconds: 0 },
              { phase: "up", point: { x: 100, y: 200 }, atOffsetSeconds: 0.05 },
            ],
          },
        ],
      });
      const call = server.calls.find((c) => c.path === "/actions/dispatch_gesture");
      assert.ok(call, "dispatch_gesture route should have been called");
      assert.deepEqual(call!.body, {
        pointers: [
          {
            id: "finger1",
            waypoints: [
              { phase: "down", x: 100, y: 200, atOffsetSeconds: 0 },
              { phase: "up", x: 100, y: 200, atOffsetSeconds: 0.05 },
            ],
          },
        ],
      });
    } finally {
      await cleanup();
    }
  });

  it("rejects multi-pointer gesture when canMultiPointer=false (host-side tier)", async () => {
    // Defence-in-depth: the host validator MUST reject before the
    // request leaves the driver — the APK has a matching guard,
    // but two gates beat one. Mirrors iOS ios.driver.ts behaviour.
    const { driver, server, cleanup } = await setup();
    try {
      await assert.rejects(
        () =>
          driver.dispatchGesture({
            pointers: [
              {
                id: "a",
                waypoints: [
                  { phase: "down", point: { x: 10, y: 20 }, atOffsetSeconds: 0 },
                  { phase: "up", point: { x: 10, y: 20 }, atOffsetSeconds: 0.05 },
                ],
              },
              {
                id: "b",
                waypoints: [
                  { phase: "down", point: { x: 30, y: 40 }, atOffsetSeconds: 0 },
                  { phase: "up", point: { x: 30, y: 40 }, atOffsetSeconds: 0.05 },
                ],
              },
            ],
          }),
        /multi-pointer/,
      );
      const leaked = server.calls.find((c) => c.path === "/actions/dispatch_gesture");
      assert.equal(leaked, undefined, "rejected gesture must not reach the APK");
    } finally {
      await cleanup();
    }
  });

  it("rejects pressure waypoint when canPressure=false", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      await assert.rejects(
        () =>
          driver.dispatchGesture({
            pointers: [
              {
                id: "f",
                waypoints: [
                  {
                    phase: "down",
                    point: { x: 10, y: 20 },
                    atOffsetSeconds: 0,
                    pressure: 0.8,
                  },
                  { phase: "up", point: { x: 10, y: 20 }, atOffsetSeconds: 0.05 },
                ],
              },
            ],
          }),
        /pressure/,
      );
      const leaked = server.calls.find((c) => c.path === "/actions/dispatch_gesture");
      assert.equal(leaked, undefined);
    } finally {
      await cleanup();
    }
  });

  it("rejects empty pointers array", async () => {
    const { driver, cleanup } = await setup();
    try {
      await assert.rejects(
        () => driver.dispatchGesture({ pointers: [] }),
        /empty pointers/,
      );
    } finally {
      await cleanup();
    }
  });

  it("serialises multi-pointer pinch when canMultiPointer=true", async () => {
    // Regression guard for the capability flip: once /ping reports
    // canMultiPointer=true, two-pointer gestures MUST serialise
    // and reach the APK. A bug that kept the host-side guard
    // rejecting multi-pointer — e.g. reading canMultiPointer off
    // a stale snapshot — would cause every pinch script to fail
    // with "multi-pointer capability unavailable" despite the
    // ping saying otherwise.
    const { driver, server, cleanup } = await setup({
      pingResponse: {
        ok: true,
        mechanism: "accessibility",
        capabilities: { canMultiPointer: true, canPressure: false },
      },
    });
    try {
      await driver.dispatchGesture({
        pointers: [
          {
            id: "f1",
            waypoints: [
              { phase: "down", point: { x: 440, y: 1200 }, atOffsetSeconds: 0 },
              { phase: "move", point: { x: 300, y: 1200 }, atOffsetSeconds: 0.5 },
              { phase: "up", point: { x: 300, y: 1200 }, atOffsetSeconds: 0.5 },
            ],
          },
          {
            id: "f2",
            waypoints: [
              { phase: "down", point: { x: 640, y: 1200 }, atOffsetSeconds: 0 },
              { phase: "move", point: { x: 780, y: 1200 }, atOffsetSeconds: 0.5 },
              { phase: "up", point: { x: 780, y: 1200 }, atOffsetSeconds: 0.5 },
            ],
          },
        ],
      });
      const call = server.calls.find((c) => c.path === "/actions/dispatch_gesture");
      assert.ok(call, "two-pointer gesture must reach the APK when capability=true");
      const body = call!.body as {
        pointers: Array<{ id: string; waypoints: Array<Record<string, unknown>> }>;
      };
      assert.equal(body.pointers.length, 2);
      assert.equal(body.pointers[0]!.id, "f1");
      assert.equal(body.pointers[1]!.id, "f2");
      // Each pointer carries three ordered waypoints. Spot-check
      // the first and last on each path to catch silent reordering
      // or dropped waypoints.
      assert.equal(body.pointers[0]!.waypoints[0]!.phase, "down");
      assert.equal(body.pointers[0]!.waypoints[0]!.x, 440);
      assert.equal(body.pointers[1]!.waypoints[2]!.phase, "up");
      assert.equal(body.pointers[1]!.waypoints[2]!.x, 780);
    } finally {
      await cleanup();
    }
  });

  it("omits pressure from wire payload when not set on waypoint", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      await driver.dispatchGesture({
        pointers: [
          {
            id: "f",
            waypoints: [
              { phase: "down", point: { x: 1, y: 2 }, atOffsetSeconds: 0 },
              { phase: "up", point: { x: 1, y: 2 }, atOffsetSeconds: 0.05 },
            ],
          },
        ],
      });
      const call = server.calls.find((c) => c.path === "/actions/dispatch_gesture");
      const wire = call!.body as {
        pointers: Array<{ waypoints: Array<Record<string, unknown>> }>;
      };
      for (const w of wire.pointers[0]!.waypoints) {
        assert.ok(
          !Object.prototype.hasOwnProperty.call(w, "pressure"),
          "waypoint must not carry a pressure key when author omits it",
        );
      }
    } finally {
      await cleanup();
    }
  });
});

describe("AndroidDriver.hideKeyboard", () => {
  it("posts to /actions/hide_keyboard and returns KeyResult", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      const result = await driver.hideKeyboard();
      assert.equal(result.ok, true);
      const call = server.calls.find((c) => c.path === "/actions/hide_keyboard");
      assert.ok(call, "hide_keyboard route should have been called");
      assert.equal(call!.method, "POST");
    } finally {
      await cleanup();
    }
  });
});
