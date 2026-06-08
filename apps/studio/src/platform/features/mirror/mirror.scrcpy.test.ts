import assert from "node:assert/strict";
import { describe, it } from "node:test";

import type { ScreenMirror } from "../../../domain/features/mirror/mirror.port.js";
import type { MirrorSession } from "../../../domain/features/mirror/mirror.types.js";
import { ScrcpyScreenMirror, scrcpyCapabilities } from "./mirror.scrcpy.js";

// `start()` is not exercised here: it constructs a Tauri `Channel`,
// which needs the webview runtime. The command surface that input
// dispatch depends on is reached through the injected invoke instead.

type InjectedInvoke = NonNullable<
  ConstructorParameters<typeof ScrcpyScreenMirror>[0]
>;

function makeFakeInvoke() {
  const calls: Array<{ cmd: string; args: Record<string, unknown> }> = [];
  const fn = ((cmd: string, args?: Record<string, unknown>) => {
    calls.push({ cmd, args: args ?? {} });
    return Promise.resolve(undefined);
  }) as unknown as InjectedInvoke;
  return { fn, calls };
}

function androidSession(overrides: Partial<MirrorSession> = {}): MirrorSession {
  return {
    id: "sess-1",
    target: { id: "emulator-5554", kind: "android", displayName: "Pixel" },
    startedAt: 0,
    backend: "scrcpy",
    videoWidth: 1080,
    videoHeight: 1920,
    capabilities: scrcpyCapabilities(),
    ...overrides,
  };
}

describe("scrcpyCapabilities", () => {
  it("advertises keyboard but neither live-typing nor pinch", () => {
    assert.deepEqual(scrcpyCapabilities(), {
      supportsRecording: false,
      supportsTouch: true,
      supportsKeyboard: true,
      supportsLiveTyping: false,
      supportsPinch: false,
    });
  });
});

describe("ScrcpyScreenMirror input routing", () => {
  it("inputText dispatches mirror_input_text with the bound device id", async () => {
    const { fn, calls } = makeFakeInvoke();
    const adapter = new ScrcpyScreenMirror(fn);
    await adapter.inputText(androidSession(), "hello");
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.cmd, "mirror_input_text");
    assert.deepEqual(calls[0]!.args, {
      sessionId: "sess-1",
      deviceId: "emulator-5554",
      text: "hello",
    });
  });

  it("eraseText forwards the count", async () => {
    const { fn, calls } = makeFakeInvoke();
    const adapter = new ScrcpyScreenMirror(fn);
    await adapter.eraseText(androidSession(), 3);
    assert.equal(calls[0]!.cmd, "mirror_erase_text");
    assert.deepEqual(calls[0]!.args, {
      sessionId: "sess-1",
      deviceId: "emulator-5554",
      count: 3,
    });
  });

  it("pressKey forwards the named key", async () => {
    const { fn, calls } = makeFakeInvoke();
    const adapter = new ScrcpyScreenMirror(fn);
    await adapter.pressKey(androidSession(), "enter");
    assert.equal(calls[0]!.cmd, "mirror_press_key");
    assert.deepEqual(calls[0]!.args, {
      sessionId: "sess-1",
      deviceId: "emulator-5554",
      key: "enter",
    });
  });

  it("longPressAt converts canvas coordinates to device ratios", async () => {
    const { fn, calls } = makeFakeInvoke();
    const adapter = new ScrcpyScreenMirror(fn);
    await adapter.longPressAt(androidSession(), { x: 540, y: 960 }, 500);
    assert.equal(calls[0]!.cmd, "mirror_simctl_long_press");
    assert.deepEqual(calls[0]!.args, {
      sessionId: "sess-1",
      deviceId: "emulator-5554",
      xRatio: 0.5,
      yRatio: 0.5,
      durationMs: 500,
      bundleId: null,
    });
  });

  it("longPressAt prefers the point's source dimensions over the session", async () => {
    const { fn, calls } = makeFakeInvoke();
    const adapter = new ScrcpyScreenMirror(fn);
    await adapter.longPressAt(
      androidSession(),
      { x: 100, y: 100, srcWidth: 400, srcHeight: 400 },
      200,
    );
    assert.equal((calls[0]!.args as { xRatio: number }).xRatio, 0.25);
    assert.equal((calls[0]!.args as { yRatio: number }).yRatio, 0.25);
  });

  it("swipe maps both endpoints to ratios", async () => {
    const { fn, calls } = makeFakeInvoke();
    const adapter = new ScrcpyScreenMirror(fn);
    await adapter.swipe(
      androidSession(),
      { x: 0, y: 0 },
      { x: 1080, y: 1920 },
      300,
    );
    assert.equal(calls[0]!.cmd, "mirror_simctl_swipe");
    assert.deepEqual(calls[0]!.args, {
      sessionId: "sess-1",
      deviceId: "emulator-5554",
      fromXRatio: 0,
      fromYRatio: 0,
      toXRatio: 1,
      toYRatio: 1,
      durationMs: 300,
      bundleId: null,
    });
  });

  it("clamps out-of-bounds ratios into the unit range", async () => {
    const { fn, calls } = makeFakeInvoke();
    const adapter = new ScrcpyScreenMirror(fn);
    await adapter.longPressAt(androidSession(), { x: 5000, y: -10 }, 100);
    assert.equal((calls[0]!.args as { xRatio: number }).xRatio, 1);
    assert.equal((calls[0]!.args as { yRatio: number }).yRatio, 0);
  });

  it("pinch rejects — Android has no simultaneous two-finger gesture", async () => {
    const adapter: ScreenMirror = new ScrcpyScreenMirror(makeFakeInvoke().fn);
    await assert.rejects(() =>
      adapter.pinch(androidSession(), { xRatio: 0.5, yRatio: 0.5 }, 1, 2, 250),
    );
  });
});
