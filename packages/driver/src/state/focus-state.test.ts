import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { findFocusedNode, findKeyboardNode, readKeyboardState } from "./focus-state.js";
import type { TreeNode } from "../tree/tree-node.js";

function n(
  attrs: Record<string, string>,
  opts: {
    focused?: boolean;
    children?: TreeNode[];
  } = {},
): TreeNode {
  return {
    attributes: attrs,
    children: opts.children ?? [],
    focused: opts.focused,
  };
}

describe("findFocusedNode", () => {
  it("returns null when no node is focused", () => {
    const tree = n({ role: "container" }, {
      children: [n({ role: "button", text: "A" }), n({ role: "button", text: "B" })],
    });
    assert.equal(findFocusedNode(tree), null);
  });

  it("returns the focused cursor when present", () => {
    const focused = n({ role: "text-field", id: "email" }, { focused: true });
    const tree = n({ role: "container" }, {
      children: [n({ role: "button", text: "A" }), focused],
    });
    const c = findFocusedNode(tree);
    assert.ok(c);
    assert.equal(c.node, focused);
  });

  it("finds deeply nested focused node", () => {
    const deep = n({ role: "text-field", id: "pin" }, { focused: true });
    const tree = n({ role: "container" }, {
      children: [
        n({ role: "container" }, {
          children: [n({ role: "container" }, { children: [deep] })],
        }),
      ],
    });
    const c = findFocusedNode(tree);
    assert.ok(c);
    assert.equal(c.node, deep);
  });

  it("returns first focused node in document order when multiple (shouldn't normally happen)", () => {
    const a = n({ id: "a" }, { focused: true });
    const b = n({ id: "b" }, { focused: true });
    const tree = n({}, { children: [a, b] });
    const c = findFocusedNode(tree);
    assert.equal(c?.node, a);
  });
});

describe("findKeyboardNode", () => {
  it("detects iOS keyboard via role", () => {
    const kb = n({ role: "keyboard", bounds: "0,600,400,900" });
    const tree = n({ role: "container" }, {
      children: [n({ role: "button" }), kb],
    });
    const c = findKeyboardNode(tree);
    assert.equal(c?.node, kb);
  });

  it("detects Android IME via ext:isIme marker", () => {
    const ime = n({ "ext:isIme": "true", bounds: "0,1400,1080,2400" });
    const tree = n({}, { children: [ime] });
    const c = findKeyboardNode(tree);
    assert.equal(c?.node, ime);
  });

  it("returns null when no keyboard node exists", () => {
    const tree = n({ role: "container" }, {
      children: [n({ role: "button" }), n({ role: "text-field" })],
    });
    assert.equal(findKeyboardNode(tree), null);
  });
});

describe("readKeyboardState", () => {
  it("returns visible=false when no keyboard", () => {
    const tree = n({ role: "container" }, {
      children: [n({ role: "button" })],
    });
    assert.deepEqual(readKeyboardState(tree), { visible: false });
  });

  it("returns visible + parsed bounds for iOS keyboard role", () => {
    const kb = n({ role: "keyboard", bounds: "0,600,400,900" });
    const tree = n({}, { children: [kb] });
    const state = readKeyboardState(tree);
    assert.equal(state.visible, true);
    assert.deepEqual(state.bounds, { left: 0, top: 600, right: 400, bottom: 900 });
  });

  it("returns visible=true without bounds when keyboard node has no bounds attr", () => {
    const kb = n({ role: "keyboard" });
    const tree = n({}, { children: [kb] });
    const state = readKeyboardState(tree);
    assert.equal(state.visible, true);
    assert.equal(state.bounds, undefined);
  });

  it("returns visible + bounds for Android ext:isIme subtree root", () => {
    const ime = n({ "ext:isIme": "true", bounds: "0,1400,1080,2400" });
    const tree = n({}, { children: [ime] });
    const state = readKeyboardState(tree);
    assert.equal(state.visible, true);
    assert.deepEqual(state.bounds, { left: 0, top: 1400, right: 1080, bottom: 2400 });
  });
});
