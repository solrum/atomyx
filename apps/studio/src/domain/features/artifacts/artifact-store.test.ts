import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { MockArtifactStore } from "./artifact-store.mock.js";

describe("ArtifactStore contract (Mock)", () => {
  const now = 1_700_000_000_000;

  it("round-trips a run through create → append → finalize → list", async () => {
    const store = new MockArtifactStore();
    await store.createRun({
      runId: "r1",
      scriptPath: "/x.yml",
      scriptName: "X",
      deviceId: "d1",
      startedAt: now,
    });
    await store.appendEvent("r1", {
      type: "stepStarted",
      stepIndex: 0,
      command: "launchApp",
    });
    await store.finalizeRun("r1", {
      completedAt: now + 5000,
      result: "passed",
      totalSteps: 1,
    });

    const runs = await store.listRuns();
    assert.equal(runs.length, 1);
    assert.equal(runs[0]!.result, "passed");

    const events: unknown[] = [];
    for await (const e of store.getEvents("r1")) events.push(e);
    assert.equal(events.length, 1);
  });

  it("saves and reads back an artifact by name", async () => {
    const store = new MockArtifactStore();
    await store.createRun({
      runId: "r2",
      scriptPath: "/y.yml",
      scriptName: "Y",
      deviceId: "d1",
      startedAt: now,
    });
    const bytes = new Uint8Array([1, 2, 3, 4]);
    const meta = await store.saveArtifact("r2", "screenshot-01.png", bytes, {
      stepIndex: 3,
      mimeType: "image/png",
    });
    assert.equal(meta.size, 4);
    assert.equal(meta.stepIndex, 3);
    const roundtrip = await store.readArtifact("r2", "screenshot-01.png");
    assert.deepEqual(roundtrip, bytes);
  });
});
