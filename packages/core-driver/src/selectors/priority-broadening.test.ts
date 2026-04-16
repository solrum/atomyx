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
