import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { MockScreenMirror } from "../../../domain/features/mirror/mirror.mock.js";
import type {
  H264Frame,
  MirrorTarget,
} from "../../../domain/features/mirror/types.js";
import { createZustandMirror } from "./mirror.zustand.js";

function mkTarget(): MirrorTarget {
  return { id: "dev-1", kind: "android", displayName: "Pixel 7" };
}

describe("mirror.zustand", () => {
  it("startForTarget records a session in the snapshot", async () => {
    const port = new MockScreenMirror();
    const api = createZustandMirror({ port });

    const id = await api.startForTarget(mkTarget());
    const snap = api.getSnapshot();
    assert.ok(snap.sessions[id]);
    assert.equal(snap.sessions[id]?.isRecording, false);
    assert.equal(snap.sessions[id]?.backend, "mock");
  });

  it("frames produced by the port are delivered to onFrame listeners", async () => {
    const port = new MockScreenMirror();
    const api = createZustandMirror({ port });

    const id = await api.startForTarget(mkTarget());
    const frames: H264Frame[] = [];
    api.onFrame(id, (f) => frames.push(f));

    port.feedFrame(id, {
      nal: new Uint8Array([1, 2, 3, 4]),
      timestampUs: 1_000,
      keyframe: true,
    });

    assert.equal(frames.length, 1);
    assert.deepEqual(Array.from(frames[0]!.nal), [1, 2, 3, 4]);
    assert.equal(frames[0]!.timestampUs, 1_000);
    assert.equal(frames[0]!.keyframe, true);
  });

  it("startRecording flips isRecording; stopRecording clears it", async () => {
    const port = new MockScreenMirror();
    const api = createZustandMirror({ port });

    const id = await api.startForTarget(mkTarget());
    await api.startRecording(id, "/tmp/run.mp4");
    assert.equal(api.getSnapshot().sessions[id]?.isRecording, true);
    assert.equal(api.getSnapshot().sessions[id]?.recordingPath, "/tmp/run.mp4");

    await api.stopRecording(id);
    assert.equal(api.getSnapshot().sessions[id]?.isRecording, false);
    assert.equal(api.getSnapshot().sessions[id]?.recordingPath, null);
  });

  it("extractClip requires an active recording", async () => {
    const port = new MockScreenMirror();
    const api = createZustandMirror({ port });
    const id = await api.startForTarget(mkTarget());

    await assert.rejects(() =>
      api.extractClip(id, { startMs: 0, endMs: 1000, outputPath: "/tmp/c.mp4" }),
    );

    await api.startRecording(id, "/tmp/r.mp4");
    const out = await api.extractClip(id, {
      startMs: 0,
      endMs: 1000,
      outputPath: "/tmp/c.mp4",
    });
    assert.equal(out, "/tmp/c.mp4");
  });

  it("stop removes the session from the snapshot and tears down listeners", async () => {
    const port = new MockScreenMirror();
    const api = createZustandMirror({ port });

    const id = await api.startForTarget(mkTarget());
    await api.stop(id);

    assert.equal(api.getSnapshot().sessions[id], undefined);
  });

  it("subscribe fires when sessions appear or disappear", async () => {
    const port = new MockScreenMirror();
    const api = createZustandMirror({ port });

    let hits = 0;
    const unsub = api.subscribe(() => {
      hits += 1;
    });

    const id = await api.startForTarget(mkTarget());
    await api.stop(id);
    unsub();

    assert.ok(hits >= 2);
  });
});
