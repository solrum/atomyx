import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { detectObscurement } from "./obscurement.js";
import {
  ancestorTrapTree,
  modalObscuredTree,
  node,
} from "../testing/fixtures.js";
import { Roles } from "../tree/tree-node.js";

describe("detectObscurement", () => {
  it("returns obscured:false when target IS topmost", () => {
    const target = node({ role: Roles.Button, id: "t", bounds: "0,0,100,50" });
    const root = node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      children: [target],
    });
    const r = detectObscurement(root, target);
    assert.equal(r.obscured, false);
  });

  it("ancestor disambiguation: UICollectionView-like wrapper is NOT an obscurer", () => {
    // `ancestorTrapTree` wraps target in a generic "Other" container
    // whose bounds contain the target. The ancestor check must
    // suppress this false positive.
    const root = ancestorTrapTree();
    // Walk to find the cell target.
    const target = root.children[0]!.children[0]!;
    const r = detectObscurement(root, target);
    assert.equal(r.obscured, false);
  });

  it("generic container with empty id+label is suppressed", () => {
    // Sibling (not ancestor) generic container that covers target.
    const target = node({ role: Roles.Cell, id: "t", bounds: "100,200,300,260" });
    const sibling = node({
      role: Roles.Other,
      // empty id + label → should be suppressed
      bounds: "0,100,430,600",
    });
    const root = node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      children: [target, sibling],
    });
    const r = detectObscurement(root, target);
    assert.equal(r.obscured, false);
  });

  it("real modal with non-empty id/label IS reported as obscurer", () => {
    const { root, target, sheet } = modalObscuredTree();
    const r = detectObscurement(root, target);
    assert.equal(r.obscured, true);
    if (r.obscured) {
      assert.equal(r.obscurer.id, "confirm-sheet");
      assert.equal(r.obscurer.label, "Confirm");
      assert.equal(r.obscurer.role, Roles.Dialog);
    }
  });

  it("returns obscured:false when target has no bounds", () => {
    const target = node({ role: Roles.Button, id: "t" });
    const root = node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      children: [target],
    });
    const r = detectObscurement(root, target);
    assert.equal(r.obscured, false);
  });

  it("later sibling with non-empty id obscures earlier target at same point", () => {
    const target = node({
      role: Roles.Button,
      id: "target",
      bounds: "100,100,300,200",
    });
    const overlay = node({
      role: Roles.Button,
      id: "overlay",
      label: "Floating action",
      bounds: "50,50,400,250",
    });
    const root = node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      // target THEN overlay — in pre-order DFS the later sibling
      // wins as the topmost covering the midpoint.
      children: [target, overlay],
    });
    const r = detectObscurement(root, target);
    assert.equal(r.obscured, true);
    if (r.obscured) {
      assert.equal(r.obscurer.id, "overlay");
    }
  });
});
