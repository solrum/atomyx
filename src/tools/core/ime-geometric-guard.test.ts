import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { CompactElement } from "../../adapters/device-controller.port.js";
import { ImeGeometricGuard } from "./ime-geometric-guard.js";

function imeKey(
  left: number,
  top: number,
  right: number,
  bottom: number,
): CompactElement {
  return {
    selector: { contentDesc: "1" },
    label: "1",
    role: "viewgroup",
    clickable: false,
    enabled: true,
    bounds: { left, top, right, bottom },
    isInIme: true,
  };
}

function nonImeElement(
  left: number,
  top: number,
  right: number,
  bottom: number,
): CompactElement {
  return {
    selector: { text: "hello" },
    label: "hello",
    role: "view",
    clickable: true,
    enabled: true,
    bounds: { left, top, right, bottom },
    isInIme: false,
  };
}

test("blocksInSummary returns true when coord is inside an IME element", () => {
  const guard = new ImeGeometricGuard();
  const summary = [imeKey(100, 200, 200, 300), nonImeElement(0, 0, 500, 100)];
  assert.equal(guard.blocksInSummary(150, 250, summary), true);
});

test("blocksInSummary returns false when coord is outside all IME elements", () => {
  const guard = new ImeGeometricGuard();
  const summary = [imeKey(100, 200, 200, 300)];
  assert.equal(guard.blocksInSummary(400, 400, summary), false);
});

test("blocksInSummary ignores non-IME elements containing the point", () => {
  const guard = new ImeGeometricGuard();
  const summary = [nonImeElement(0, 0, 1000, 1000)]; // full screen, not IME
  assert.equal(guard.blocksInSummary(500, 500, summary), false);
});

test("blocks async returns true on IME hit", async () => {
  const guard = new ImeGeometricGuard();
  const ctl = {
    getUiSummary: async () => [imeKey(0, 100, 100, 200)],
  };
  assert.equal(await guard.blocks(50, 150, ctl), true);
});

test("blocks async returns false when controller throws", async () => {
  const guard = new ImeGeometricGuard();
  const ctl = {
    getUiSummary: async () => {
      throw new Error("offline");
    },
  };
  assert.equal(await guard.blocks(50, 150, ctl), false);
});
