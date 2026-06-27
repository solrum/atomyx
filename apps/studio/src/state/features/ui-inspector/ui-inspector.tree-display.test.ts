import { test } from "node:test";
import { strict as assert } from "node:assert";
import type { UiTreeNode } from "../../../domain/features/runtime/index.js";
import { summarize, truncate } from "./ui-inspector-tree-display.js";

function node(
  attrs: Readonly<Record<string, string>>,
): UiTreeNode {
  return { attributes: attrs, children: [] };
}

test("summarize", async (t) => {
  await t.test("prefers text over label and id", () => {
    assert.equal(
      summarize(node({ class: "staticText", text: "Hi", label: "L", id: "x" })),
      'staticText "Hi"',
    );
  });

  await t.test("falls back to label when no text", () => {
    assert.equal(
      summarize(node({ class: "button", label: "Sign in", id: "x" })),
      "button (Sign in)",
    );
  });

  await t.test("falls back to id when no text/label", () => {
    assert.equal(summarize(node({ class: "view", id: "main" })), "view #main");
  });

  await t.test("returns class alone when no semantic attribute", () => {
    assert.equal(summarize(node({ class: "other" })), "other");
  });

  await t.test("uses role when class is absent", () => {
    assert.equal(summarize(node({ role: "button" })), "button");
  });

  await t.test('falls back to "node" when neither class nor role exist', () => {
    assert.equal(summarize(node({})), "node");
  });

  await t.test("falls back to short class when role is 'other' (Android Flutter view)", () => {
    // Role pin "other" carries no information by itself — show
    // the platform-native short class name so the row is grep-able.
    assert.equal(
      summarize(node({ role: "other", class: "android.view.View" })),
      "View",
    );
  });

  await t.test("preserves Android long class under showRaw when fallback shortens", () => {
    const out = summarize(
      node({ role: "other", class: "android.view.View" }),
      true,
    );
    assert.equal(out, "View · android.view.View");
  });

  await t.test("returns 'other' when role is 'other' and no class is present", () => {
    assert.equal(summarize(node({ role: "other" })), "other");
  });

  await t.test("truncates long text with ellipsis", () => {
    const long = "a".repeat(60);
    const out = summarize(node({ class: "staticText", text: long }));
    assert.ok(out.endsWith('…"'));
    assert.ok(out.length < 60);
  });
});

test("truncate", async (t) => {
  await t.test("returns short strings unchanged", () => {
    assert.equal(truncate("hello", 10), "hello");
  });

  await t.test("returns the input when length equals max", () => {
    assert.equal(truncate("abcde", 5), "abcde");
  });

  await t.test("appends an ellipsis when shortened", () => {
    assert.equal(truncate("abcdef", 5), "abcd…");
  });
});
