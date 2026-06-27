import { test } from "node:test";
import { strict as assert } from "node:assert";
import { formatRelativeTime } from "./runs-format-relative-time.js";

const NOW = Date.UTC(2026, 0, 1, 12, 0, 0);

test("formatRelativeTime", async (t) => {
  await t.test("uses seconds bucket below one minute", () => {
    assert.equal(formatRelativeTime(NOW - 5_000, NOW), "5s ago");
    assert.equal(formatRelativeTime(NOW - 59_000, NOW), "59s ago");
  });

  await t.test("uses minutes bucket below one hour", () => {
    assert.equal(formatRelativeTime(NOW - 60_000, NOW), "1m ago");
    assert.equal(formatRelativeTime(NOW - 59 * 60_000, NOW), "59m ago");
  });

  await t.test("uses hours bucket below one day", () => {
    assert.equal(formatRelativeTime(NOW - 60 * 60_000, NOW), "1h ago");
    assert.equal(formatRelativeTime(NOW - 23 * 3600_000, NOW), "23h ago");
  });

  await t.test("uses days bucket beyond one day", () => {
    assert.equal(formatRelativeTime(NOW - 24 * 3600_000, NOW), "1d ago");
    assert.equal(formatRelativeTime(NOW - 7 * 86_400_000, NOW), "7d ago");
  });

  await t.test("clamps future timestamps to 0s ago", () => {
    assert.equal(formatRelativeTime(NOW + 30_000, NOW), "0s ago");
  });
});
