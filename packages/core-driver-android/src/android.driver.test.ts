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
    // at an already-listening server.
    await (this as unknown as { http: { get: (p: string) => Promise<unknown> } }).http.get("/health");
    (this as unknown as { connected: boolean }).connected = true;
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
async function setup(): Promise<{
  server: Awaited<ReturnType<typeof startFakeServer>>;
  driver: TestAndroidDriver;
  cleanup: () => Promise<void>;
}> {
  const server = await startFakeServer((call, res) => {
    res.setHeader("content-type", "application/json");
    if (call.path === "/health") {
      res.end(JSON.stringify({ ok: true, accessibilityConnected: true }));
      return;
    }
    if (call.path === "/tree") {
      res.end(
        JSON.stringify({
          elementId: "root",
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
      assert.equal(tree.attributes["role"], "container");
      assert.equal(tree.children.length, 1);
      const btn = tree.children[0]!;
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

describe("AndroidDriver.inputText + pressKey", () => {
  it("posts inputText to /actions/type_keyboard", async () => {
    const { driver, server, cleanup } = await setup();
    try {
      await driver.inputText("hello");
      const call = server.calls.find((c) => c.path === "/actions/type_keyboard");
      assert.deepEqual(call!.body, { text: "hello" });
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
  it("reports canEraseText=false and canWaitForIdle=false for legacy APK", async () => {
    const { driver, cleanup } = await setup();
    try {
      const caps = driver.capabilities;
      assert.equal(caps.canEraseText, false);
      assert.equal(caps.canWaitForIdle, false);
      assert.equal(caps.canScreenshot, true);
    } finally {
      await cleanup();
    }
  });
});
