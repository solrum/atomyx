import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { FakeClock } from "@atomyx/core/infra";
import { MockDriver } from "../testing/mock-driver.js";
import type { TreeNode } from "../tree/tree-node.js";
import { waitForTreeStable } from "./wait-for-tree-stable.js";
import { WaitTimeoutError } from "./wait-until.js";

function node(attrs: Record<string, string>, children: TreeNode[] = []): TreeNode {
  return { attributes: attrs, children };
}

describe("waitForTreeStable", () => {
  it("returns the stable tree after quietMs of no changes", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    // Same tree staged 5 times → hash never changes → stable once
    // quietMs elapses since the first observation.
    const stable = node({ role: "container", bounds: "0,0,400,900" });
    driver.stageHierarchyRepeated(stable, 10);

    const pending = waitForTreeStable({
      driver,
      quietMs: 100,
      clock,
      timeoutMs: 1000,
      intervalMs: 50,
    });
    await tick();
    // First fetch records hash, sets stableSince=0.
    clock.advance(50);
    await tick();
    // Second fetch: hash unchanged, now-stableSince=50 < 100, keep waiting.
    clock.advance(60);
    await tick();
    // Third fetch: now=110, stableSince=0, diff=110 >= 100 → resolve.
    const tree = await pending;
    assert.equal(tree.attributes["role"], "container");
  });

  it("resets stableSince when the tree changes", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(node({ role: "container", state: "a" }));
    driver.stageHierarchy(node({ role: "container", state: "b" }));
    driver.stageHierarchy(node({ role: "container", state: "c" }));
    driver.stageHierarchyRepeated(node({ role: "container", state: "c" }), 5);

    const pending = waitForTreeStable({
      driver,
      quietMs: 100,
      clock,
      timeoutMs: 2000,
      intervalMs: 50,
    });
    await tick();
    clock.advance(50);
    await tick();
    clock.advance(50);
    await tick();
    clock.advance(50);
    await tick();
    clock.advance(80);
    await tick();
    const tree = await pending;
    assert.equal(tree.attributes["state"], "c");
  });

  it("throws when tree keeps changing past timeout", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    // Stage many different trees so the hash changes every poll.
    for (let i = 0; i < 20; i++) {
      driver.stageHierarchy(node({ role: "container", frame: String(i) }));
    }
    const pending = waitForTreeStable({
      driver,
      quietMs: 100,
      clock,
      timeoutMs: 200,
      intervalMs: 50,
    });
    const assertion = assert.rejects(pending, WaitTimeoutError);
    for (let i = 0; i < 10; i++) {
      await tick();
      clock.advance(50);
    }
    await assertion;
  });
});

async function tick(): Promise<void> {
  await new Promise((r) => setImmediate(r));
}
