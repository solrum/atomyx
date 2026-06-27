import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { hashVisibleLeaves, countLeaves, maxLeafBoundsBottom } from "./tree-signature.js";
import { node } from "../testing/fixtures.js";
import { Roles } from "../tree/tree-node.js";

function leafNode(text: string, bounds: string) {
  return node({ role: Roles.Cell, text, bounds });
}

function containerWith(...children: ReturnType<typeof node>[]) {
  return node({ role: Roles.Container, bounds: "0,0,430,932", children });
}

describe("hashVisibleLeaves", () => {
  it("same tree produces the same hash", () => {
    const tree = containerWith(
      leafNode("Hello", "0,100,430,140"),
      leafNode("World", "0,200,430,240"),
    );
    assert.equal(hashVisibleLeaves(tree), hashVisibleLeaves(tree));
  });

  it("different bounds → different hash", () => {
    const treeA = containerWith(leafNode("Hello", "0,100,430,140"));
    const treeB = containerWith(leafNode("Hello", "0,200,430,240"));
    assert.notEqual(hashVisibleLeaves(treeA), hashVisibleLeaves(treeB));
  });

  it("leaf order does not affect hash (deterministic sort)", () => {
    const treeAB = containerWith(
      leafNode("A", "0,100,430,140"),
      leafNode("B", "0,200,430,240"),
    );
    const treeBA = containerWith(
      leafNode("B", "0,200,430,240"),
      leafNode("A", "0,100,430,140"),
    );
    assert.equal(hashVisibleLeaves(treeAB), hashVisibleLeaves(treeBA));
  });

  it("adding a leaf changes the hash", () => {
    const one = containerWith(leafNode("A", "0,100,430,140"));
    const two = containerWith(
      leafNode("A", "0,100,430,140"),
      leafNode("B", "0,200,430,240"),
    );
    assert.notEqual(hashVisibleLeaves(one), hashVisibleLeaves(two));
  });

  it("root with no children is itself a leaf and returns a stable hash", () => {
    const empty = node({ role: Roles.Container, bounds: "0,0,430,932" });
    const h1 = hashVisibleLeaves(empty);
    const h2 = hashVisibleLeaves(empty);
    assert.equal(h1, h2);
    assert.ok(typeof h1 === "string" && h1.length > 0);
  });
});

describe("countLeaves", () => {
  it("node with no children counts as 1 leaf", () => {
    const leaf = leafNode("X", "0,0,100,100");
    assert.equal(countLeaves(leaf), 1);
  });

  it("container with N leaf children = N", () => {
    const tree = containerWith(
      leafNode("A", "0,100,430,140"),
      leafNode("B", "0,200,430,240"),
      leafNode("C", "0,300,430,340"),
    );
    assert.equal(countLeaves(tree), 3);
  });

  it("nested tree counts only leaf nodes", () => {
    // root → group → [leaf1, leaf2]
    const group = node({ role: Roles.Container, bounds: "0,0,430,500", children: [
      leafNode("L1", "0,100,430,140"),
      leafNode("L2", "0,200,430,240"),
    ]});
    const root = node({ role: Roles.Container, bounds: "0,0,430,932", children: [group] });
    assert.equal(countLeaves(root), 2);
  });
});

describe("maxLeafBoundsBottom", () => {
  it("returns 0 when no leaf has bounds", () => {
    const empty = node({ role: Roles.Container });
    assert.equal(maxLeafBoundsBottom(empty), 0);
  });

  it("returns the bottom of the single leaf", () => {
    const tree = containerWith(leafNode("A", "0,100,430,240"));
    assert.equal(maxLeafBoundsBottom(tree), 240);
  });

  it("returns the maximum bottom across multiple leaves", () => {
    const tree = containerWith(
      leafNode("A", "0,100,430,140"),
      leafNode("B", "0,500,430,820"),
      leafNode("C", "0,200,430,300"),
    );
    assert.equal(maxLeafBoundsBottom(tree), 820);
  });
});
