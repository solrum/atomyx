import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { FakeClock } from "@atomyx/core/infra";
import { waitUntil, WaitTimeoutError } from "./wait-until.js";

describe("waitUntil", () => {
  it("returns immediately when the first fetch satisfies the predicate", async () => {
    const clock = new FakeClock();
    let calls = 0;
    const result = await waitUntil<number>({
      fetch: async () => {
        calls += 1;
        return 42;
      },
      predicate: (v) => v === 42,
      timeoutMs: 100,
      intervalMs: 10,
      clock,
    });
    assert.equal(result, 42);
    assert.equal(calls, 1);
  });

  it("polls until the predicate matches", async () => {
    const clock = new FakeClock();
    let value = 0;
    const pending = waitUntil<number>({
      fetch: async () => {
        value += 1;
        return value;
      },
      predicate: (v) => v === 3,
      timeoutMs: 1000,
      intervalMs: 50,
      clock,
    });
    // Walk the clock so the sleep promises resolve.
    await tick(); // let fetch 1 land
    clock.advance(50);
    await tick(); // fetch 2
    clock.advance(50);
    await tick(); // fetch 3 — predicate now true
    const result = await pending;
    assert.equal(result, 3);
  });

  it("throws WaitTimeoutError when deadline passes", async () => {
    const clock = new FakeClock();
    const pending = waitUntil<number>({
      fetch: async () => 0,
      predicate: (v) => v === 42,
      timeoutMs: 100,
      intervalMs: 50,
      clock,
      kind: "test",
    });
    // Attach a silencing handler up-front so rejection isn't
    // flagged as "unhandled" while we drive the clock forward.
    const assertion = assert.rejects(pending, (err: Error) => {
      assert.ok(err instanceof WaitTimeoutError);
      assert.match(err.message, /test/);
      return true;
    });
    await tick();
    clock.advance(50);
    await tick();
    clock.advance(60);
    await tick();
    await assertion;
  });

  it("checks once at timeoutMs=0 and resolves if satisfied", async () => {
    const clock = new FakeClock();
    const result = await waitUntil<string>({
      fetch: async () => "ok",
      predicate: (v) => v === "ok",
      timeoutMs: 0,
      clock,
    });
    assert.equal(result, "ok");
  });

  it("throws at timeoutMs=0 when predicate is never satisfied", async () => {
    const clock = new FakeClock();
    await assert.rejects(
      waitUntil<string>({
        fetch: async () => "no",
        predicate: (v) => v === "ok",
        timeoutMs: 0,
        clock,
      }),
      WaitTimeoutError,
    );
  });
});

/** Flush microtasks so scheduled sleeps register before `advance` runs. */
async function tick(): Promise<void> {
  await new Promise((r) => setImmediate(r));
}
