import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { compileSelector } from "./priority-broadening.js";
import { fromTree } from "../filters/element-filter.js";
import { AttrKeys, Roles } from "../tree/tree-node.js";
import { node } from "../testing/fixtures.js";

describe("compileSelector priority broadening", () => {
  const tree = node({
    role: Roles.Container,
    children: [
      node({
        role: Roles.Button,
        id: "login_btn",
        text: "Sign in",
        label: "Login button",
        enabled: true,
        clickable: true,
      }),
      node({
        role: Roles.Button,
        id: "cancel_btn",
        text: "Cancel",
        enabled: true,
        clickable: true,
      }),
      node({
        role: Roles.Cell,
        text: "Sign in",
      }),
      node({
        role: Roles.TextField,
        id: "email",
        hint: "Email address",
        enabled: true,
      }),
    ],
  });

  it("matches by id when id provided", () => {
    const f = compileSelector({ id: "login_btn" });
    const r = f(fromTree(tree));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "login_btn");
  });

  it("matches by text when no id — text is in priority chain", () => {
    const f = compileSelector({ text: "Cancel" });
    const r = f(fromTree(tree));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "cancel_btn");
  });

  it("AND-s role constraint with content filter", () => {
    // "Sign in" matches TWO nodes (button + cell); role=button
    // narrows to one.
    const f = compileSelector({ text: "Sign in", role: Roles.Button });
    const r = f(fromTree(tree));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "login_btn");
  });

  it("priority order: id wins when both id and text provided", () => {
    // Even though text="Sign in" would also work, id is tried
    // first and produces a non-empty result, so text is never
    // consulted. Verify by giving a text that would NOT match
    // the element with the id — if priority broadening fell
    // through, we'd see the id match anyway.
    const f = compileSelector({ id: "login_btn", text: "nonexistent" });
    const r = f(fromTree(tree));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "login_btn");
  });

  it("priority falls through when the higher-priority field misses", () => {
    const f = compileSelector({ id: "nope", text: "Cancel" });
    const r = f(fromTree(tree));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "cancel_btn");
  });

  it("label priority is between id and text", () => {
    const f = compileSelector({ label: "Login button" });
    const r = f(fromTree(tree));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "login_btn");
  });

  it("hint works for inputs", () => {
    const f = compileSelector({ hint: "Email address" });
    const r = f(fromTree(tree));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "email");
  });

  it("nth selects from broadened result", () => {
    // Two nodes have text "Sign in" (button + cell). With nth=1,
    // pick the second (cell).
    const f = compileSelector({ text: "Sign in", nth: 1 });
    const r = f(fromTree(tree));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Role], Roles.Cell);
  });

  it("empty content + constraints returns constraint-only matches", () => {
    const f = compileSelector({ role: Roles.Button });
    const r = f(fromTree(tree));
    assert.equal(r.length, 2);
  });

  it("regex patterns work across fields", () => {
    const f = compileSelector({ text: /^cancel$/i });
    const r = f(fromTree(tree));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "cancel_btn");
  });
});

describe("compileSelector candidate ranking", () => {
  // Tree where a non-clickable title AND a clickable button both
  // carry text "Verify". Without ranking, tree order returns the
  // title (first in document order) — wrong. With ranking,
  // clickable wins.
  function duplicateLabelTree() {
    return node({
      role: Roles.Container,
      children: [
        node({ role: Roles.Text, text: "Verify" }), // non-clickable title
        node({
          role: Roles.Button,
          id: "verify_btn",
          text: "Verify",
          enabled: true,
          clickable: true,
        }),
      ],
    });
  }

  it("ranks clickable above non-clickable when both match", () => {
    const f = compileSelector({ text: "Verify" });
    const r = f(fromTree(duplicateLabelTree()));
    assert.equal(r.length, 2);
    // First result is the clickable button.
    assert.equal(r[0]!.node.clickable, true);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "verify_btn");
  });

  it("skips ranking when nth is provided — preserves tree order", () => {
    // With nth=0 the caller meant the first in document order, NOT
    // the highest-scored candidate. Verify title (non-clickable)
    // wins here because it appears first in the tree.
    const f = compileSelector({ text: "Verify", nth: 0 });
    const r = f(fromTree(duplicateLabelTree()));
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Role], Roles.Text);
    assert.equal(r[0]!.node.clickable, undefined);
  });

  it("ranks focused above non-focused with equal clickability", () => {
    const tree = node({
      role: Roles.Container,
      children: [
        node({
          role: Roles.TextField,
          id: "a",
          hint: "Email",
          bounds: "0,0,400,60",
        }),
        node({
          role: Roles.TextField,
          id: "b",
          hint: "Email",
          bounds: "0,100,400,160",
          focused: true,
        }),
      ],
    });
    const f = compileSelector({ hint: "Email" });
    const r = f(fromTree(tree));
    assert.equal(r.length, 2);
    assert.equal(r[0]!.node.focused, true);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "b");
  });

  it("preserves document order when scores are equal (stable sort)", () => {
    const tree = node({
      role: Roles.Container,
      children: [
        node({
          role: Roles.Button,
          id: "a",
          text: "Click",
          clickable: true,
        }),
        node({
          role: Roles.Button,
          id: "b",
          text: "Click",
          clickable: true,
        }),
      ],
    });
    const f = compileSelector({ text: "Click" });
    const r = f(fromTree(tree));
    assert.equal(r.length, 2);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "a");
    assert.equal(r[1]!.node.attributes[AttrKeys.Id], "b");
  });
});
