import { test } from "node:test";
import { strict as assert } from "node:assert";
import { defaultRecordingPath } from "./mirror-recording-path.js";

test("defaultRecordingPath", async (t) => {
  await t.test("renders ISO timestamp with safe separators", () => {
    const out = defaultRecordingPath({
      target: { id: "iphone-15" },
      startedAt: Date.UTC(2026, 0, 1, 12, 30, 45, 250),
    });
    assert.equal(out, "mirror-iphone-15-2026-01-01T12-30-45-250Z.mp4");
  });

  await t.test("differentiates sessions on the same device by start time", () => {
    const a = defaultRecordingPath({
      target: { id: "iphone-15" },
      startedAt: 1_700_000_000_000,
    });
    const b = defaultRecordingPath({
      target: { id: "iphone-15" },
      startedAt: 1_700_000_001_000,
    });
    assert.notEqual(a, b);
  });
});
