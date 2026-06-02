import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { MockScreenMirror } from "../../../domain/features/mirror/mirror.mock.js";
import type { MirrorTarget } from "../../../domain/features/mirror/types.js";
import { ScreenMirrorDispatcher } from "./mirror.dispatcher.js";

function mkTarget(
  kind: MirrorTarget["kind"],
  id = `${kind}-1`,
): MirrorTarget {
  return { id, kind, displayName: id };
}

describe("mirror.dispatcher", () => {
  it("routes start() to the adapter registered for the target kind", async () => {
    const android = new MockScreenMirror();
    const iosSim = new MockScreenMirror();
    const iosDevice = new MockScreenMirror();
    const dispatcher = new ScreenMirrorDispatcher({
      android,
      "ios-simulator": iosSim,
      "ios-device": iosDevice,
    });

    const sessionA = await dispatcher.start(mkTarget("android"));
    const sessionI = await dispatcher.start(mkTarget("ios-simulator"));

    assert.equal(android.activeSessions().length, 1);
    assert.equal(iosSim.activeSessions().length, 1);
    assert.equal(iosDevice.activeSessions().length, 0);
    assert.notEqual(sessionA.id, sessionI.id);
  });

  it("throws on start() when target kind has no adapter", async () => {
    const dispatcher = new ScreenMirrorDispatcher({
      android: new MockScreenMirror(),
    } as never);
    await assert.rejects(() => dispatcher.start(mkTarget("ios-device")));
  });

  it("routes stop() + onFrame() + record() to the originating adapter", async () => {
    const android = new MockScreenMirror();
    const iosSim = new MockScreenMirror();
    const iosDevice = new MockScreenMirror();
    const dispatcher = new ScreenMirrorDispatcher({
      android,
      "ios-simulator": iosSim,
      "ios-device": iosDevice,
    });

    const session = await dispatcher.start(mkTarget("android"));
    let frames = 0;
    dispatcher.onFrame(session, () => {
      frames += 1;
    });
    android.feedFrame(session.id, {
      nal: new Uint8Array([0, 0, 0, 1]),
      timestampUs: 0,
      keyframe: true,
    });
    assert.equal(frames, 1);

    await dispatcher.record(session, "/tmp/x.mp4");
    assert.ok(android.hasRecording(session.id));

    await dispatcher.stop(session);
    assert.equal(android.activeSessions().length, 0);
  });

  it("rejects operations on untracked sessions", async () => {
    const dispatcher = new ScreenMirrorDispatcher({
      android: new MockScreenMirror(),
    } as never);
    const foreign = {
      id: "ghost",
      target: mkTarget("android"),
      startedAt: 0,
      backend: "mock",
    } as const;
    assert.throws(() => dispatcher.onFrame(foreign, () => undefined));
    await assert.rejects(() => dispatcher.stop(foreign));
  });
});
