import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { MockDriver } from "./mock-driver.js";
import { node } from "./fixtures.js";
import { Roles } from "../tree/tree-node.js";

describe("MockDriver — abort signal propagation", () => {
  it("rejects every method synchronously when the signal is already aborted", async () => {
    const driver = new MockDriver();
    driver.stageHierarchy(node({ role: Roles.Container, bounds: "0,0,10,10" }));
    const controller = new AbortController();
    controller.abort();

    // A representative cross-section of the surface — covering
    // hierarchy (read), tap (gesture), inputText (text), and
    // launchApp (lifecycle) — to confirm the abort check fires
    // before any internal state mutation.
    await assert.rejects(driver.hierarchy({ signal: controller.signal }), {
      name: "AbortError",
    });
    await assert.rejects(driver.tap({ x: 1, y: 2 }, { signal: controller.signal }), {
      name: "AbortError",
    });
    await assert.rejects(driver.inputText("hi", { signal: controller.signal }), {
      name: "AbortError",
    });
    await assert.rejects(driver.launchApp("com.example", undefined, { signal: controller.signal }), {
      name: "AbortError",
    });
    // The pre-aborted calls must NOT have consumed the staged tree
    // or recorded gesture calls.
    assert.equal(driver.calls.length, 0);
  });

  it("hangs hierarchy() and resolves only via signal abort when hangOnNextHierarchy is set", async () => {
    const driver = new MockDriver();
    driver.stageHierarchy(node({ role: Roles.Container, bounds: "0,0,10,10" }));
    driver.hangOnNextHierarchy = true;
    const controller = new AbortController();
    const startedAt = Date.now();
    const pending = driver.hierarchy({ signal: controller.signal });
    setTimeout(() => controller.abort(new DOMException("wrapper deadline", "AbortError")), 30);
    const err = await pending.catch((e) => e);
    const elapsedMs = Date.now() - startedAt;
    assert.ok(elapsedMs < 500, `expected fast abort, got ${elapsedMs}ms`);
    assert.equal((err as Error).name, "AbortError");
    assert.match((err as Error).message, /wrapper deadline/);
    // hangOnNextHierarchy auto-resets so the staged tree is still
    // available for the next call.
    assert.equal(driver.hangOnNextHierarchy, false);
  });

  it("ignores opts.signal when not aborted — calls proceed normally", async () => {
    const driver = new MockDriver();
    driver.stageHierarchy(node({ role: Roles.Container, bounds: "0,0,10,10" }));
    const controller = new AbortController(); // never aborts
    await driver.tap({ x: 1, y: 2 }, { signal: controller.signal });
    const tree = await driver.hierarchy({ signal: controller.signal });
    assert.equal(driver.calls.filter((c) => c.method === "tap").length, 1);
    assert.ok(tree); // tree returned, signal unused
  });
});
