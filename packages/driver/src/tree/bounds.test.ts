import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  parseBounds,
  formatBounds,
  boundsCenter,
  boundsContain,
  boundsIntersect,
} from "./bounds.js";

describe("Bounds", () => {
  it("parseBounds accepts canonical format", () => {
    assert.deepEqual(parseBounds("0,10,100,60"), {
      left: 0,
      top: 10,
      right: 100,
      bottom: 60,
    });
  });

  it("parseBounds rejects malformed input", () => {
    assert.equal(parseBounds(undefined), null);
    assert.equal(parseBounds(""), null);
    assert.equal(parseBounds("1,2,3"), null);
    assert.equal(parseBounds("a,b,c,d"), null);
  });

  it("formatBounds round-trips", () => {
    const b = { left: 5, top: 10, right: 25, bottom: 40 };
    assert.deepEqual(parseBounds(formatBounds(b)), b);
  });

  it("boundsCenter computes midpoint", () => {
    assert.deepEqual(boundsCenter({ left: 0, top: 0, right: 100, bottom: 50 }), {
      x: 50,
      y: 25,
    });
  });

  it("boundsContain is half-open (right/bottom exclusive)", () => {
    const b = { left: 10, top: 20, right: 30, bottom: 40 };
    assert.equal(boundsContain(b, 10, 20), true);
    assert.equal(boundsContain(b, 29, 39), true);
    assert.equal(boundsContain(b, 30, 40), false);
    assert.equal(boundsContain(b, 9, 20), false);
  });

  it("boundsIntersect detects overlap", () => {
    const a = { left: 0, top: 0, right: 10, bottom: 10 };
    assert.equal(boundsIntersect(a, { left: 5, top: 5, right: 15, bottom: 15 }), true);
    assert.equal(boundsIntersect(a, { left: 10, top: 0, right: 20, bottom: 10 }), false);
    assert.equal(boundsIntersect(a, { left: 20, top: 20, right: 30, bottom: 30 }), false);
  });
});
