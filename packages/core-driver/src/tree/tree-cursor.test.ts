import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { walk, ancestorsOf, descendantsOf } from "./tree-cursor.js";
import type { TreeNode } from "./tree-node.js";

function node(id: string, children: TreeNode[] = []): TreeNode {
  return {
    attributes: { id },
    children,
  };
}

describe("TreeCursor walk", () => {
  it("yields root only for leaf tree", () => {
    const root = node("a");
    const cursors = walk(root);
    assert.equal(cursors.length, 1);
    assert.equal(cursors[0]!.node, root);
    assert.equal(cursors[0]!.parent, null);
    assert.equal(cursors[0]!.depth, 0);
  });

  it("yields nodes in pre-order DFS", () => {
    // tree:  a -> [b -> [d, e], c -> [f]]
    const d = node("d");
    const e = node("e");
    const f = node("f");
    const b = node("b", [d, e]);
    const c = node("c", [f]);
    const a = node("a", [b, c]);
    const cursors = walk(a);
    const ids = cursors.map((c) => c.node.attributes.id);
    assert.deepEqual(ids, ["a", "b", "d", "e", "c", "f"]);
  });

  it("sets parent pointers correctly", () => {
    const d = node("d");
    const b = node("b", [d]);
    const a = node("a", [b]);
    const cursors = walk(a);
    const dCursor = cursors.find((c) => c.node === d)!;
    assert.equal(dCursor.parent?.node, b);
    assert.equal(dCursor.parent?.parent?.node, a);
    assert.equal(dCursor.parent?.parent?.parent, null);
    assert.equal(dCursor.depth, 2);
  });

  it("childIndex reflects sibling position", () => {
    const x = node("x");
    const y = node("y");
    const z = node("z");
    const root = node("root", [x, y, z]);
    const cursors = walk(root);
    assert.equal(cursors.find((c) => c.node === x)!.childIndex, 0);
    assert.equal(cursors.find((c) => c.node === y)!.childIndex, 1);
    assert.equal(cursors.find((c) => c.node === z)!.childIndex, 2);
  });
});

describe("ancestorsOf / descendantsOf", () => {
  it("ancestorsOf walks up to root", () => {
    const d = node("d");
    const b = node("b", [d]);
    const a = node("a", [b]);
    const cursors = walk(a);
    const dCursor = cursors.find((c) => c.node === d)!;
    const chain = ancestorsOf(dCursor).map((c) => c.node.attributes.id);
    assert.deepEqual(chain, ["b", "a"]);
  });

  it("descendantsOf excludes the cursor itself", () => {
    const d = node("d");
    const e = node("e");
    const b = node("b", [d, e]);
    const cursors = walk(b);
    const bCursor = cursors[0]!;
    const descendants = descendantsOf(bCursor).map((c) => c.node.attributes.id);
    assert.deepEqual(descendants, ["d", "e"]);
    assert.equal(descendants.includes("b"), false);
  });
});
