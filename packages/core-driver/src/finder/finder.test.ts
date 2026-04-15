import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Finder, FindTimeoutError } from "./finder.js";
import { FakeClock } from "../infra/clock.port.js";
import { MockDriver } from "../testing/mock-driver.js";
import { node } from "../testing/fixtures.js";
import { Roles } from "../tree/tree-node.js";
import { compileSelector } from "../selectors/priority-broadening.js";

/**
 * Drain the microtask queue enough times for the finder's
 * awaited chain (driver.hierarchy → filter → deadline check →
 * clock.sleep) to reach the next suspend point. 20 iterations
 * is overkill but cheap and deterministic.
 */
async function flushMicrotasks(): Promise<void> {
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

/**
 * Advance the fake clock by `ms` and flush microtasks both
 * before and after so pending sleeps resolve, the awaiter
 * resumes, the next hierarchy call chains through, and the
 * next sleep is registered — all before we advance again.
 */
async function tick(clock: FakeClock, ms: number): Promise<void> {
  await flushMicrotasks();
  clock.advance(ms);
  await flushMicrotasks();
}

function emptyTree() {
  return node({ role: Roles.Container });
}

function treeWithLoginButton() {
  return node({
    role: Roles.Container,
    children: [
      node({ role: Roles.Button, id: "login", text: "Log in" }),
    ],
  });
}

describe("Finder.find", () => {
  it("returns matches from a single hierarchy call", async () => {
    const driver = new MockDriver().stageHierarchy(treeWithLoginButton());
    const clock = new FakeClock();
    const finder = new Finder({ driver, clock });
    const r = await finder.find(compileSelector({ id: "login" }));
    assert.equal(r.length, 1);
    assert.equal(driver.calls.filter((c) => c.method === "hierarchy").length, 1);
  });

  it("returns empty array when no match", async () => {
    const driver = new MockDriver().stageHierarchy(emptyTree());
    const finder = new Finder({ driver, clock: new FakeClock() });
    const r = await finder.find(compileSelector({ id: "nope" }));
    assert.deepEqual(r, []);
  });
});

describe("Finder.findOne", () => {
  it("returns null on no match", async () => {
    const driver = new MockDriver().stageHierarchy(emptyTree());
    const finder = new Finder({ driver, clock: new FakeClock() });
    const r = await finder.findOne(compileSelector({ id: "nope" }));
    assert.equal(r, null);
  });

  it("returns first match", async () => {
    const driver = new MockDriver().stageHierarchy(treeWithLoginButton());
    const finder = new Finder({ driver, clock: new FakeClock() });
    const r = await finder.findOne(compileSelector({ id: "login" }));
    assert.ok(r);
    assert.equal(r!.node.attributes.id, "login");
  });
});

describe("Finder.waitFor", () => {
  it("resolves on the first poll when element already present", async () => {
    const driver = new MockDriver().stageHierarchy(treeWithLoginButton());
    const clock = new FakeClock();
    const finder = new Finder({ driver, clock });
    const r = await finder.waitFor(compileSelector({ id: "login" }), {
      timeoutMs: 1000,
    });
    assert.equal(r.length, 1);
    assert.equal(driver.calls.filter((c) => c.method === "hierarchy").length, 1);
  });

  it("polls repeatedly until element appears", async () => {
    const driver = new MockDriver()
      .stageHierarchy(emptyTree())
      .stageHierarchy(emptyTree())
      .stageHierarchy(treeWithLoginButton());
    const clock = new FakeClock();
    const finder = new Finder({ driver, clock });

    const promise = finder.waitFor(compileSelector({ id: "login" }), {
      timeoutMs: 5000,
      pollIntervalMs: 100,
    });

    // Two sleeps are needed between poll 1→2 and poll 2→3.
    await tick(clock, 100);
    await tick(clock, 100);

    const r = await promise;
    assert.equal(r.length, 1);
    const hierarchyCalls = driver.calls.filter((c) => c.method === "hierarchy").length;
    assert.equal(hierarchyCalls, 3);
  });

  it("throws FindTimeoutError on timeout", async () => {
    const driver = new MockDriver()
      .stageHierarchyRepeated(emptyTree(), 100);
    const clock = new FakeClock();
    const finder = new Finder({ driver, clock });

    const promise = finder.waitFor(compileSelector({ id: "nope" }), {
      timeoutMs: 500,
      pollIntervalMs: 100,
    }).catch((err) => err);

    // 500ms budget ÷ 100ms interval → at most 6 polls before
    // the deadline check throws. Drive the loop generously.
    for (let i = 0; i < 8; i++) {
      await tick(clock, 100);
    }
    const result = await promise;
    assert.ok(result instanceof FindTimeoutError);
  });
});
