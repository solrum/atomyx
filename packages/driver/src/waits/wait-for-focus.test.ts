import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { FakeClock } from "@atomyx/core/infra";
import { MockDriver } from "../testing/mock-driver.js";
import type { TreeNode } from "../tree/tree-node.js";
import { waitForFocus } from "./wait-for-focus.js";
import { WaitTimeoutError } from "./wait-until.js";

function node(
  attrs: Record<string, string>,
  opts: { focused?: boolean; clickable?: boolean; children?: TreeNode[] } = {},
): TreeNode {
  return {
    attributes: attrs,
    children: opts.children ?? [],
    focused: opts.focused,
    clickable: opts.clickable,
  };
}

describe("waitForFocus", () => {
  it("returns immediately when target is already focused", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, {
        children: [
          node({ role: "text-field", id: "email", bounds: "0,100,400,160" }, {
            focused: true,
          }),
        ],
      }),
    );
    const cursor = await waitForFocus({
      driver,
      selector: { id: "email" },
      clock,
      timeoutMs: 500,
    });
    assert.equal(cursor.node.attributes["id"], "email");
  });

  it("polls until the target gains focus", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    const unfocused = node({ role: "container" }, {
      children: [
        node({ role: "text-field", id: "email", bounds: "0,100,400,160" }),
      ],
    });
    const focused = node({ role: "container" }, {
      children: [
        node({ role: "text-field", id: "email", bounds: "0,100,400,160" }, {
          focused: true,
        }),
      ],
    });
    driver.stageHierarchy(unfocused);
    driver.stageHierarchy(unfocused);
    driver.stageHierarchy(focused);

    const pending = waitForFocus({
      driver,
      selector: { id: "email" },
      clock,
      timeoutMs: 500,
      intervalMs: 50,
    });
    await tick();
    clock.advance(50);
    await tick();
    clock.advance(50);
    await tick();
    const cursor = await pending;
    assert.equal(cursor.node.attributes["id"], "email");
  });

  it("matches via bounds intersection (Flutter merged semantics)", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    // Outer wrapper matches the selector; inner (a11y-merged) is the
    // one carrying focused=true. Bounds are identical — intersect
    // tolerates that.
    const tree = node({ role: "container" }, {
      children: [
        node(
          { role: "text-field", id: "pin", bounds: "0,100,400,160" },
          {
            children: [
              node({ role: "text-field", bounds: "0,100,400,160" }, {
                focused: true,
              }),
            ],
          },
        ),
      ],
    });
    driver.stageHierarchy(tree);
    const cursor = await waitForFocus({
      driver,
      selector: { id: "pin" },
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["id"], "pin");
  });

  it("throws WaitTimeoutError when target never focuses", async () => {
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
    const pending = waitForFocus({
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
