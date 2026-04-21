import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { FakeClock } from "@atomyx/core/infra";
import { MockDriver } from "../testing/mock-driver.js";
import type { TreeNode } from "../tree/tree-node.js";
import { waitForKeyboard } from "./wait-for-keyboard.js";
import { WaitTimeoutError } from "./wait-until.js";

function node(attrs: Record<string, string>, children: TreeNode[] = []): TreeNode {
  return { attributes: attrs, children };
}

describe("waitForKeyboard", () => {
  it("returns immediately when the keyboard state already matches", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({ role: "keyboard", bounds: "0,600,400,900" }),
      ]),
    );
    const state = await waitForKeyboard({
      driver,
      expectVisible: true,
      clock,
      timeoutMs: 100,
    });
    assert.equal(state.visible, true);
    assert.deepEqual(state.bounds, { left: 0, top: 600, right: 400, bottom: 900 });
  });

  it("waits for the keyboard to disappear", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({ role: "keyboard", bounds: "0,600,400,900" }),
      ]),
    );
    driver.stageHierarchy(node({ role: "container" }));
    const pending = waitForKeyboard({
      driver,
      expectVisible: false,
      clock,
      timeoutMs: 500,
      intervalMs: 50,
    });
    await tick();
    clock.advance(50);
    await tick();
    const state = await pending;
    assert.equal(state.visible, false);
  });

  it("detects Android IME subtree root via ext:isIme", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({ "ext:isIme": "true", bounds: "0,1400,1080,2400" }),
      ]),
    );
    const state = await waitForKeyboard({
      driver,
      expectVisible: true,
      clock,
      timeoutMs: 100,
    });
    assert.equal(state.visible, true);
  });

  it("throws when the keyboard never reaches expected state", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchyRepeated(
      node({ role: "container" }, [node({ role: "keyboard" })]),
      10,
    );
    const pending = waitForKeyboard({
      driver,
      expectVisible: false,
      clock,
      timeoutMs: 100,
      intervalMs: 50,
    });
    const assertion = assert.rejects(pending, WaitTimeoutError);
    for (let i = 0; i < 5; i++) {
      await tick();
      clock.advance(50);
    }
    await assertion;
  });
});

async function tick(): Promise<void> {
  await new Promise((r) => setImmediate(r));
}
