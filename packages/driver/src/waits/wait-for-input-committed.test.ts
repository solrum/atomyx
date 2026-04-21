import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { FakeClock } from "@atomyx/core/infra";
import { MockDriver } from "../testing/mock-driver.js";
import type { TreeNode } from "../tree/tree-node.js";
import { waitForInputCommitted } from "./wait-for-input-committed.js";
import { WaitTimeoutError } from "./wait-until.js";

function node(attrs: Record<string, string>, children: TreeNode[] = []): TreeNode {
  return { attributes: attrs, children };
}

const FIELD_BOUNDS = { left: 24, top: 100, right: 400, bottom: 160 };

describe("waitForInputCommitted", () => {
  it("exact matches for regular text-field by anchor bounds", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({
          role: "text-field",
          id: "email",
          text: "a@b.com",
          bounds: "24,100,400,160",
        }),
      ]),
    );
    const cursor = await waitForInputCommitted({
      driver,
      anchorBounds: FIELD_BOUNDS,
      expected: "a@b.com",
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["text"], "a@b.com");
  });

  it("matches by length for secure-text-field (masked bullets)", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({
          role: "secure-text-field",
          id: "password",
          text: "•••••••••",
          bounds: "24,100,400,160",
        }),
      ]),
    );
    const cursor = await waitForInputCommitted({
      driver,
      anchorBounds: FIELD_BOUNDS,
      expected: "secret123",
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["text"], "•••••••••");
  });

  it("relocates the field across a text-field → secure-text-field role transition", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    // Anchor bounds captured BEFORE typing (field was still
    // text-field). After typing, role flipped to secure-text-field
    // but bounds are identical. waitForInputCommitted must follow
    // the bounds rather than the stale role.
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({
          role: "secure-text-field",
          id: "password",
          text: "•••",
          bounds: "24,100,400,160",
        }),
      ]),
    );
    const cursor = await waitForInputCommitted({
      driver,
      anchorBounds: FIELD_BOUNDS,
      expected: "xyz",
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["role"], "secure-text-field");
  });

  it("picks the smallest intersecting input when multiple match", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    // Wrapper container (role=container) has text-field descendants;
    // two of them intersect the anchor, but the actual field is
    // smaller. Smallest bounds wins.
    driver.stageHierarchy(
      node({ role: "container", bounds: "0,0,800,800" }, [
        node({
          role: "text-field",
          id: "outer",
          text: "outer-text",
          bounds: "0,50,600,400", // intersects anchor, too big
        }),
        node({
          role: "text-field",
          id: "inner",
          text: "inner-text",
          bounds: "24,100,400,160", // matches anchor exactly
        }),
      ]),
    );
    const cursor = await waitForInputCommitted({
      driver,
      anchorBounds: FIELD_BOUNDS,
      expected: "inner-text",
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["id"], "inner");
  });

  it("accepts partial masked content for secure field (typing in progress)", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    // Field shows 6 bullets but we expected 9 — still accept,
    // because the content is non-placeholder (real typing landed).
    // The alternative (strict length match) causes false-negative
    // retries on iOS Flutter where Semantics value lags behind.
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({
          role: "secure-text-field",
          id: "password",
          text: "••••••",
          label: "Password",
          bounds: "24,100,400,160",
        }),
      ]),
    );
    const cursor = await waitForInputCommitted({
      driver,
      anchorBounds: FIELD_BOUNDS,
      expected: "secret123",
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["text"], "••••••");
  });

  it("rejects when secure field still shows the placeholder", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchyRepeated(
      node({ role: "container" }, [
        node({
          role: "secure-text-field",
          id: "password",
          text: "Password",
          label: "Password",
          bounds: "24,100,400,160",
        }),
      ]),
      10,
    );
    const pending = waitForInputCommitted({
      driver,
      anchorBounds: FIELD_BOUNDS,
      expected: "secret123",
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

  it("accepts partial text match for regular field (Flutter Semantics lag)", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    // Typed "otp@example.com" but tree only shows "otp@example.co".
    // Text differs from placeholder → typing committed, accept.
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({
          role: "text-field",
          id: "email",
          text: "otp@example.co",
          label: "Email",
          bounds: "24,100,400,160",
        }),
      ]),
    );
    const cursor = await waitForInputCommitted({
      driver,
      anchorBounds: FIELD_BOUNDS,
      expected: "otp@example.com",
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["text"], "otp@example.co");
  });

  it("rejects when regular field still shows the placeholder", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchyRepeated(
      node({ role: "container" }, [
        node({
          role: "text-field",
          id: "email",
          text: "Email",
          label: "Email",
          bounds: "24,100,400,160",
        }),
      ]),
      10,
    );
    const pending = waitForInputCommitted({
      driver,
      anchorBounds: FIELD_BOUNDS,
      expected: "otp@example.com",
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

  it("counts unicode code points in expected string for secure fields", async () => {
    const driver = new MockDriver();
    const clock = new FakeClock();
    driver.stageHierarchy(
      node({ role: "container" }, [
        node({
          role: "secure-text-field",
          id: "pin",
          text: "••••",
          bounds: "24,100,400,160",
        }),
      ]),
    );
    // 4 code points: "1😀2a"
    const cursor = await waitForInputCommitted({
      driver,
      anchorBounds: FIELD_BOUNDS,
      expected: "1😀2a",
      clock,
      timeoutMs: 100,
    });
    assert.equal(cursor.node.attributes["text"], "••••");
  });
});

async function tick(): Promise<void> {
  await new Promise((r) => setImmediate(r));
}
