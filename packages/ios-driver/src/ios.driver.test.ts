import { describe, it } from "node:test";
import assert from "node:assert/strict";
import net from "node:net";
import type { AddressInfo } from "node:net";
import { IosDriver } from "./ios.driver.js";

/**
 * IosDriver tests run against a fake TCP server that speaks the
 * Swift driver wire protocol. The driver is instantiated with
 * `kind: "simulator"` so it never tries to spawn iproxy — tests
 * bypass the transport tunnel entirely and exercise only the
 * command dispatch + tree normalization paths.
 */

interface Call {
  type: string;
  args: Record<string, unknown>;
}

async function setup(
  handler: (call: Call) => { ok: boolean; data?: unknown; error?: string },
): Promise<{
  driver: IosDriver;
  calls: Call[];
  close: () => Promise<void>;
}> {
  const calls: Call[] = [];
  const server = net.createServer((sock) => {
    sock.setEncoding("utf8");
    let buf = "";
    sock.on("data", (chunk: string) => {
      buf += chunk;
      let nl: number;
      while ((nl = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, nl);
        buf = buf.slice(nl + 1);
        if (!line) continue;
        const req = JSON.parse(line) as {
          id: number;
          type: string;
          args: Record<string, unknown>;
        };
        calls.push({ type: req.type, args: req.args });
        const result = handler({ type: req.type, args: req.args });
        sock.write(
          JSON.stringify({
            id: req.id,
            ok: result.ok,
            data: result.data,
            error: result.error,
          }) + "\n",
        );
      }
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", () => resolve()));
  const port = (server.address() as AddressInfo).port;
  const driver = new IosDriver({
    kind: "simulator",
    udid: "FAKE-UDID",
    port,
    connectTimeoutMs: 2000,
  });
  await driver.connect();
  return {
    driver,
    calls,
    close: async () => {
      await driver.disconnect();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    },
  };
}

// Default handler — every command returns {ok:true, data:{}}
// unless specialized. The `hierarchy()` path needs a raw tree,
// so tests that exercise it stage their own handler.
function okHandler(): { ok: boolean; data: Record<string, unknown> } {
  return { ok: true, data: {} };
}

describe("IosDriver lifecycle", () => {
  it("ping handshake during connect", async () => {
    const { calls, close } = await setup(okHandler);
    try {
      // First call in connect() is the ping handshake.
      assert.ok(calls.some((c) => c.type === "ping"));
    } finally {
      await close();
    }
  });
});

describe("IosDriver.hierarchy", () => {
  it("dispatches dumpRawTree and returns canonical TreeNode", async () => {
    const { driver, calls, close } = await setup((call) => {
      if (call.type === "ping") return { ok: true, data: {} };
      if (call.type === "dumpRawTree") {
        return {
          ok: true,
          data: {
            root: {
              elementType: "window",
              bounds: { left: 0, top: 0, right: 430, bottom: 932 },
              children: [
                {
                  elementType: "button",
                  identifier: "login",
                  label: "Sign in",
                  bounds: { left: 100, top: 400, right: 330, bottom: 460 },
                  enabled: true,
                },
              ],
            },
          },
        };
      }
      return { ok: true, data: {} };
    });
    try {
      const tree = await driver.hierarchy();
      assert.equal(tree.attributes["role"], "container");
      assert.equal(tree.children.length, 1);
      const btn = tree.children[0]!;
      assert.equal(btn.attributes["id"], "login");
      assert.equal(btn.attributes["label"], "Sign in");
      // Button is not a text leaf; `text` is intentionally absent
      // so the dump distinguishes view+a11y from staticText leaves.
      assert.equal(btn.attributes["text"], undefined);
      assert.equal(btn.attributes["role"], "button");
      assert.equal(btn.clickable, true);
      // calls should include dumpRawTree
      assert.ok(calls.some((c) => c.type === "dumpRawTree"));
    } finally {
      await close();
    }
  });
});

describe("IosDriver gestures", () => {
  it("tap posts tapAt with {x, y}", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.tap({ x: 215, y: 430 });
      const tap = calls.find((c) => c.type === "tapAt");
      assert.ok(tap);
      assert.equal(tap!.args.x, 215);
      assert.equal(tap!.args.y, 430);
    } finally {
      await close();
    }
  });

  it("longPress posts longPressAt with durationMs", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.longPress({ x: 100, y: 200 }, 800);
      const lp = calls.find((c) => c.type === "longPressAt");
      assert.deepEqual(lp!.args, { x: 100, y: 200, durationMs: 800 });
    } finally {
      await close();
    }
  });

  it("swipe posts from/to as flat fields", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.swipe({ x: 10, y: 20 }, { x: 30, y: 40 }, 250);
      const swipe = calls.find((c) => c.type === "swipe");
      assert.deepEqual(swipe!.args, {
        fromX: 10,
        fromY: 20,
        toX: 30,
        toY: 40,
        durationMs: 250,
      });
    } finally {
      await close();
    }
  });
});

describe("IosDriver input + keys", () => {
  it("inputText dispatches typeText", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.inputText("hello");
      assert.ok(calls.some((c) => c.type === "typeText" && c.args.text === "hello"));
    } finally {
      await close();
    }
  });

  it("eraseText dispatches clearFocusedInput with maxDeletes arg", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.eraseText(25);
      const clear = calls.find((c) => c.type === "clearFocusedInput");
      assert.ok(clear);
      // Arg name must match Swift's `ClearFocusedInputCommand`
      // which reads `maxDeletes`. A prior version sent `maxKeys`
      // which Swift silently ignored, falling back to the
      // default 100 — regression test for that contract slip.
      assert.equal(clear!.args.maxDeletes, 25);
    } finally {
      await close();
    }
  });

  it("pressKey maps affordanceFound+strategy into KeyResult (verifiable path)", async () => {
    const { driver, close } = await setup((call) => {
      if (call.type === "ping") return { ok: true, data: {} };
      if (call.type === "pressKey") {
        // Real Swift PressKeyCommand response shape.
        return {
          ok: true,
          data: { key: "back", affordanceFound: true, strategy: "nav_bar_back" },
        };
      }
      return { ok: true, data: {} };
    });
    try {
      const result = await driver.pressKey("back");
      assert.equal(result.ok, true);
      assert.equal(result.reason, "nav_bar_back");
    } finally {
      await close();
    }
  });

  it("pressKey maps affordanceFound=false to ok=false (edge-swipe fallback)", async () => {
    const { driver, close } = await setup((call) => {
      if (call.type === "ping") return { ok: true, data: {} };
      if (call.type === "pressKey") {
        return {
          ok: true,
          data: {
            key: "back",
            affordanceFound: false,
            strategy: "edge_swipe_best_effort",
          },
        };
      }
      return { ok: true, data: {} };
    });
    try {
      const result = await driver.pressKey("back");
      assert.equal(result.ok, false);
      assert.equal(result.reason, "edge_swipe_best_effort");
    } finally {
      await close();
    }
  });

  it("pressKey home reports home strategy with affordanceFound=true", async () => {
    const { driver, close } = await setup((call) => {
      if (call.type === "ping") return { ok: true, data: {} };
      if (call.type === "pressKey") {
        return {
          ok: true,
          data: { key: "home", affordanceFound: true, strategy: "home" },
        };
      }
      return { ok: true, data: {} };
    });
    try {
      const result = await driver.pressKey("home");
      assert.equal(result.ok, true);
      assert.equal(result.reason, "home");
    } finally {
      await close();
    }
  });
});

describe("IosDriver app lifecycle", () => {
  it("launchApp dispatches launchApp with bundleId and tracks foreground", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.launchApp("com.example.app");
      const launch = calls.find((c) => c.type === "launchApp");
      assert.equal(launch!.args.bundleId, "com.example.app");

      const fg = await driver.currentForeground();
      assert.equal(fg.bundleId, "com.example.app");
    } finally {
      await close();
    }
  });

  it("launchApp with noReset:true skips TCP when bundleId already tracked", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.launchApp("com.example.app");
      const launchCallsBefore = calls.filter((c) => c.type === "launchApp").length;

      await driver.launchApp("com.example.app", { noReset: true });
      const launchCallsAfter = calls.filter((c) => c.type === "launchApp").length;

      assert.equal(launchCallsAfter, launchCallsBefore);
    } finally {
      await close();
    }
  });

  it("launchApp with noReset:true relaunches when bundleId differs", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.launchApp("com.example.first");
      await driver.launchApp("com.example.second", { noReset: true });
      const launches = calls.filter((c) => c.type === "launchApp");
      assert.equal(launches.length, 2);
      assert.equal(launches[1]!.args.bundleId, "com.example.second");
      assert.equal(launches[1]!.args.noReset, true);
    } finally {
      await close();
    }
  });

  it("launchApp without noReset always dispatches even when already tracked", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.launchApp("com.example.app");
      await driver.launchApp("com.example.app");
      const launches = calls.filter((c) => c.type === "launchApp");
      assert.equal(launches.length, 2);
    } finally {
      await close();
    }
  });

  it("stopApp dispatches forceStopApp and clears foreground when matching", async () => {
    const { driver, close } = await setup(okHandler);
    try {
      await driver.launchApp("com.a");
      await driver.stopApp("com.a");
      const fg = await driver.currentForeground();
      assert.equal(fg.bundleId, null);
    } finally {
      await close();
    }
  });

  it("killApp delegates to stopApp", async () => {
    const { driver, calls, close } = await setup(okHandler);
    try {
      await driver.killApp("com.z");
      // killApp → stopApp → forceStopApp
      assert.ok(calls.some((c) => c.type === "forceStopApp" && c.args.bundleId === "com.z"));
    } finally {
      await close();
    }
  });
});

describe("IosDriver media + device info", () => {
  it("screenshot decodes base64 into Uint8Array", async () => {
    const { driver, close } = await setup((call) => {
      if (call.type === "ping") return { ok: true, data: {} };
      if (call.type === "screenshot") {
        return {
          ok: true,
          data: { base64: Buffer.from("png-bytes").toString("base64") },
        };
      }
      return { ok: true, data: {} };
    });
    try {
      const bytes = await driver.screenshot();
      assert.equal(Buffer.from(bytes).toString(), "png-bytes");
    } finally {
      await close();
    }
  });

  it("screenSize dispatches getScreenSize", async () => {
    const { driver, close } = await setup((call) => {
      if (call.type === "ping") return { ok: true, data: {} };
      if (call.type === "getScreenSize") {
        return { ok: true, data: { width: 430, height: 932 } };
      }
      return { ok: true, data: {} };
    });
    try {
      const size = await driver.screenSize();
      assert.equal(size.width, 430);
      assert.equal(size.height, 932);
    } finally {
      await close();
    }
  });

  it("deviceInfo returns simulator kind for kind=simulator", async () => {
    const { driver, close } = await setup(okHandler);
    try {
      const info = await driver.deviceInfo();
      assert.equal(info.platform, "ios");
      assert.equal(info.kind, "simulator");
      assert.equal(info.udid, "FAKE-UDID");
    } finally {
      await close();
    }
  });
});

describe("IosDriver listApps", () => {
  it("returns empty list — composition layer is responsible", async () => {
    const { driver, close } = await setup(okHandler);
    try {
      const apps = await driver.listApps();
      assert.deepEqual(apps, []);
    } finally {
      await close();
    }
  });
});

describe("IosDriver capabilities", () => {
  it("reports canEraseText=true (Swift driver has clearFocusedInput)", async () => {
    const { driver, close } = await setup(okHandler);
    try {
      assert.equal(driver.capabilities.canEraseText, true);
      assert.equal(driver.capabilities.canWaitForIdle, false);
      assert.equal(driver.capabilities.canScreenshot, true);
      assert.equal(driver.capabilities.canHideKeyboard, true);
    } finally {
      await close();
    }
  });
});

describe("IosDriver.hideKeyboard", () => {
  it("dispatches hideKeyboard command and returns KeyResult", async () => {
    const { driver, calls, close } = await setup((call) => {
      if (call.type === "hideKeyboard") {
        return { ok: true, data: { ok: true, strategy: "hide-affordance" } };
      }
      return { ok: true, data: {} };
    });
    try {
      const result = await driver.hideKeyboard();
      assert.equal(result.ok, true);
      assert.equal(result.reason, "hide-affordance");
      const hideCall = calls.find((c) => c.type === "hideKeyboard");
      assert.ok(hideCall, "hideKeyboard command should have been dispatched");
    } finally {
      await close();
    }
  });

  it("reports ok=false when agent returns ok=false", async () => {
    const { driver, close } = await setup((call) => {
      if (call.type === "hideKeyboard") {
        return { ok: true, data: { ok: false, strategy: "no-keyboard-visible" } };
      }
      return { ok: true, data: {} };
    });
    try {
      const result = await driver.hideKeyboard();
      assert.equal(result.ok, false);
      assert.equal(result.reason, "no-keyboard-visible");
    } finally {
      await close();
    }
  });
});
