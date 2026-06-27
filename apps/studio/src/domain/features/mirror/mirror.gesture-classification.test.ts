import { test } from "node:test";
import { strict as assert } from "node:assert";
import {
  LONG_PRESS_MS,
  TAP_TOLERANCE_PX,
  classifyGesture,
} from "./mirror-gesture-classification.js";

test("classifyGesture", async (t) => {
  await t.test("stationary short press is a tap", () => {
    assert.equal(classifyGesture(0, 100), "tap");
    assert.equal(classifyGesture(TAP_TOLERANCE_PX, 100), "tap");
  });

  await t.test("stationary long press is a long-press", () => {
    assert.equal(classifyGesture(0, LONG_PRESS_MS), "long-press");
    assert.equal(classifyGesture(TAP_TOLERANCE_PX, LONG_PRESS_MS + 200), "long-press");
  });

  await t.test("displacement beyond tolerance is a swipe regardless of hold", () => {
    assert.equal(classifyGesture(TAP_TOLERANCE_PX + 1, 50), "swipe");
    assert.equal(classifyGesture(200, LONG_PRESS_MS + 1_000), "swipe");
  });

  await t.test("boundary at tolerance keeps gesture stationary", () => {
    assert.equal(classifyGesture(TAP_TOLERANCE_PX, LONG_PRESS_MS - 1), "tap");
  });
});
