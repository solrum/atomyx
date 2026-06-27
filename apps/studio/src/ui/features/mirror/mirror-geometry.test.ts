import { test } from "node:test";
import { strict as assert } from "node:assert";
import {
  deviceRectToScreenRect,
  parseBoundsAttribute,
  type MirrorFrameLayout,
} from "./mirror-geometry.js";

test("parseBoundsAttribute", async (t) => {
  await t.test("parses standard integer form", () => {
    assert.deepEqual(parseBoundsAttribute("0,0,1080,1920"), {
      x: 0,
      y: 0,
      w: 1080,
      h: 1920,
    });
  });

  await t.test("parses non-origin rect", () => {
    assert.deepEqual(parseBoundsAttribute("120,240,480,320"), {
      x: 120,
      y: 240,
      w: 360,
      h: 80,
    });
  });

  await t.test("rejects malformed inputs", () => {
    assert.equal(parseBoundsAttribute(undefined), null);
    assert.equal(parseBoundsAttribute(""), null);
    assert.equal(parseBoundsAttribute("1,2,3"), null);
    assert.equal(parseBoundsAttribute("a,b,c,d"), null);
    assert.equal(parseBoundsAttribute("10,10,5,5"), null);
  });
});

test("deviceRectToScreenRect", async (t) => {
  const identity: MirrorFrameLayout = {
    scale: 1,
    offsetX: 0,
    offsetY: 0,
    drawnW: 1000,
    drawnH: 2000,
    srcWidth: 1000,
    srcHeight: 2000,
  };

  await t.test("identity mapping preserves rect", () => {
    const out = deviceRectToScreenRect(
      { x: 100, y: 200, w: 50, h: 80 },
      identity,
    );
    assert.deepEqual(out, { left: 100, top: 200, width: 50, height: 80 });
  });

  await t.test("letterbox adds offset, scale shrinks rect", () => {
    const layout: MirrorFrameLayout = {
      scale: 0.5,
      offsetX: 20,
      offsetY: 0,
      drawnW: 500,
      drawnH: 1000,
      srcWidth: 1000,
      srcHeight: 2000,
    };
    const out = deviceRectToScreenRect({ x: 100, y: 200, w: 50, h: 80 }, layout);
    assert.deepEqual(out, {
      left: 20 + 50,
      top: 100,
      width: 25,
      height: 40,
    });
  });

  await t.test("degenerate layout returns null", () => {
    const out = deviceRectToScreenRect(
      { x: 0, y: 0, w: 10, h: 10 },
      { ...identity, scale: 0, drawnW: 0, drawnH: 0 },
    );
    assert.equal(out, null);
  });

  await t.test("tree extent rescales device pixels onto video frame", () => {
    const layout: MirrorFrameLayout = {
      scale: 1,
      offsetX: 0,
      offsetY: 0,
      drawnW: 480,
      drawnH: 1080,
      srcWidth: 480,
      srcHeight: 1080,
    };
    const out = deviceRectToScreenRect(
      { x: 540, y: 1170, w: 540, h: 1170 },
      layout,
      { width: 1080, height: 2340 },
    );
    assert.deepEqual(out, {
      left: 240,
      top: 540,
      width: 240,
      height: 540,
    });
  });

  await t.test("extent + letterbox compose", () => {
    const layout: MirrorFrameLayout = {
      scale: 0.5,
      offsetX: 10,
      offsetY: 20,
      drawnW: 240,
      drawnH: 540,
      srcWidth: 480,
      srcHeight: 1080,
    };
    const out = deviceRectToScreenRect(
      { x: 1080, y: 2340, w: 1080, h: 2340 },
      layout,
      { width: 2160, height: 4680 },
    );
    assert.deepEqual(out, {
      left: 10 + 120,
      top: 20 + 270,
      width: 120,
      height: 270,
    });
  });
});
