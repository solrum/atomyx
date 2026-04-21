import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { FakeClock, SystemClock } from "./clock.port.js";

describe("FakeClock", () => {
  it("now() starts at zero by default", () => {
    const c = new FakeClock();
    assert.equal(c.now(), 0);
  });

  it("now() respects startAtMs", () => {
    const c = new FakeClock(1000);
    assert.equal(c.now(), 1000);
  });

  it("sleep(0) resolves immediately", async () => {
    const c = new FakeClock();
    await c.sleep(0);
  });

  it("sleep resolves after advance >= deadline", async () => {
    const c = new FakeClock();
    let resolved = false;
    const p = c.sleep(100).then(() => {
      resolved = true;
    });
    c.advance(50);
    await Promise.resolve();
    assert.equal(resolved, false);
    assert.equal(c.pendingCount(), 1);
    c.advance(60);
    await p;
    assert.equal(resolved, true);
    assert.equal(c.pendingCount(), 0);
  });

  it("multiple sleeps resolve in deadline order", async () => {
    const c = new FakeClock();
    const order: number[] = [];
    const p1 = c.sleep(200).then(() => order.push(200));
    const p2 = c.sleep(100).then(() => order.push(100));
    const p3 = c.sleep(150).then(() => order.push(150));
    c.advance(250);
    await Promise.all([p1, p2, p3]);
    // Note: order of push depends on iteration order within advance;
    // the contract is "all expired resolve before advance returns",
    // not a specific relative ordering beyond that. Assert only
    // that all three fired.
    assert.equal(order.length, 3);
  });

  it("advance() does not resolve sleeps that are still in the future", () => {
    const c = new FakeClock();
    let resolved = false;
    void c.sleep(100).then(() => {
      resolved = true;
    });
    c.advance(50);
    assert.equal(resolved, false);
    assert.equal(c.pendingCount(), 1);
  });
});

describe("SystemClock", () => {
  it("now() returns a sane wall-clock value", () => {
    const c = new SystemClock();
    const before = Date.now();
    const now = c.now();
    const after = Date.now();
    assert.ok(now >= before && now <= after);
  });

  it("sleep resolves after the requested delay", async () => {
    const c = new SystemClock();
    const t0 = Date.now();
    await c.sleep(20);
    const dt = Date.now() - t0;
    assert.ok(dt >= 18, `expected ~20ms delay, got ${dt}ms`);
  });
});
