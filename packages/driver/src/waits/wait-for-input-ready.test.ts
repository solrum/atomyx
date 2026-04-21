import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { FakeClock } from "@atomyx/core/infra";
import { MockDriver } from "../testing/mock-driver.js";
import type { TreeNode } from "../tree/tree-node.js";
import { waitForInputReady } from "./wait-for-input-ready.js";
import { WaitTimeoutError } from "./wait-until.js";

function node(
  attrs: Record<string, string>,
  opts: { focused?: boolean; children?: TreeNode[] } = {},
): TreeNode {
  return {
    attributes: attrs,
    children: opts.children ?? [],
    focused: opts.focused,
  };
}

describe("waitForInputReady", () => {
  it("accepts when selector target reports focused=true (strict signal)", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, {
        children: [
          node(
            { role: "text-field", id: "email", bounds: "0,100,400,160" },
            { focused: true },
          ),
        ],
      }),
    );
    await waitForInputReady({
      driver,
      selector: { id: "email" },
      clock,
      timeoutMs: 100,
    });
  });

  it("accepts when keyboard is visible and no focused node (Flutter iOS fallback)", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, {
        children: [
          node({ role: "text-field", id: "email", bounds: "0,100,400,160" }),
          node({ role: "keyboard", bounds: "0,600,400,900" }),
        ],
      }),
    );
    await waitForInputReady({
      driver,
      selector: { id: "email" },
      clock,
      timeoutMs: 100,
    });
  });

  it("rejects when a DIFFERENT node is focused (stale focus)", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    // A different field reports focused=true, but the keyboard is
    // also visible. waitForInputReady should keep waiting rather
    // than fall through to the keyboard-fallback and accept a
    // wrong-target focus.
    driver.stageHierarchyRepeated(
      node({ role: "container" }, {
        children: [
          node(
            { role: "text-field", id: "other", bounds: "0,0,400,60" },
            { focused: true },
          ),
          node({ role: "text-field", id: "email", bounds: "0,100,400,160" }),
          node({ role: "keyboard", bounds: "0,600,400,900" }),
        ],
      }),
      10,
    );
    const pending = waitForInputReady({
      driver,
      selector: { id: "email" },
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

  it("rejects when neither focused-node nor keyboard is observed", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchyRepeated(
      node({ role: "container" }, {
        children: [
          node({ role: "text-field", id: "email", bounds: "0,100,400,160" }),
        ],
      }),
      10,
    );
    const pending = waitForInputReady({
      driver,
      selector: { id: "email" },
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
