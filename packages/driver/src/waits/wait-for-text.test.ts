import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { FakeClock } from "@atomyx/core/infra";
import { MockDriver } from "../testing/mock-driver.js";
import type { TreeNode } from "../tree/tree-node.js";
import { waitForText } from "./wait-for-text.js";
import { WaitTimeoutError } from "./wait-until.js";

function node(attrs: Record<string, string>, children: TreeNode[] = []): TreeNode {
  return { attributes: attrs, children };
}

describe("waitForText", () => {
  it("returns when text already matches", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({ role: "text-field", id: "email", text: "a@b.com", bounds: "0,0,400,60" }),
      ]),
    );
    const cursor = await waitForText({
      driver,
      selector: { id: "email" },
      expected: "a@b.com",
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["text"], "a@b.com");
  });

  it("accepts a RegExp pattern", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({ role: "text-field", id: "otp", text: "123456", bounds: "0,0,400,60" }),
      ]),
    );
    const cursor = await waitForText({
      driver,
      selector: { id: "otp" },
      expected: /^\d{6}$/,
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["text"], "123456");
  });

  it("polls until the text updates", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({ role: "text-field", id: "email", bounds: "0,0,400,60" }),
      ]),
    );
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({ role: "text-field", id: "email", text: "a", bounds: "0,0,400,60" }),
      ]),
    );
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({ role: "text-field", id: "email", text: "a@b.com", bounds: "0,0,400,60" }),
      ]),
    );
    const pending = waitForText({
      driver,
      selector: { id: "email" },
      expected: "a@b.com",
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
    assert.equal(cursor.node.attributes["text"], "a@b.com");
  });

  it("throws when text never matches", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchyRepeated(
      node({ role: "container" }, [
        node({ role: "text-field", id: "email", text: "wrong", bounds: "0,0,400,60" }),
      ]),
      10,
    );
    const pending = waitForText({
      driver,
      selector: { id: "email" },
      expected: "right",
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
