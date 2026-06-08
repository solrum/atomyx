import type { UiTreeNode } from "../../../domain/features/runtime/index.js";
import type { UiNodePath } from "./ui-inspector.contract.js";

/**
 * Pure tree-filtering helpers consumed by the inspector UI. Live
 * in the state layer (alongside the inspector's other tree
 * helpers) rather than in the UI component so the logic can be
 * unit-tested under `node:test` and replaced without touching the
 * React render path.
 *
 * Two responsibilities:
 *
 *   - `isInformative` — is a node carry semantic value worth
 *     surfacing on its own? Used to dim noise rows in raw mode.
 *
 *   - `collectInterestingPaths` — pre-pass over a tree producing
 *     the set of paths that should be rendered when "hide noise"
 *     is enabled. A path is interesting iff the node itself
 *     carries id/text/label OR any descendant does.
 *
 * Why "interest" beats per-node heuristics:
 *
 *   XCUITest dumps wrap every container in `other` rows that
 *   carry no identifier; SwiftUI/UIKit also pad layouts with
 *   spacer leaves and class-only `window` / `container` rows
 *   that have no labelled descendants. Per-node rules
 *   ("non-informative leaf") catch only the easy cases — the
 *   transitive set catches dead branches uniformly: every wrapper
 *   collapses iff its entire subtree is unlabelled.
 */
export function isInformative(node: UiTreeNode): boolean {
  const attrs = node.attributes;
  if (attrs["id"] || attrs["text"] || attrs["label"]) return true;
  const cls = attrs["class"];
  if (!cls || cls === "other" || cls === "node") return false;
  return true;
}

/**
 * Build the set of paths that should be rendered when hide-noise
 * is on. Run once per tree update; the render path performs an
 * O(1) lookup against the returned Set per row. Path encoding
 * matches the index path used by the React tree component
 * (`""` for root, `"0.1.2"` for the third grandchild of the
 * second child of the first child of the root).
 */
export function collectInterestingPaths(
  tree: UiTreeNode | null,
): ReadonlySet<UiNodePath> {
  const set = new Set<UiNodePath>();
  if (!tree) return set;
  collectInterestingPathsRecursive(tree, "", set);
  return set;
}

function collectInterestingPathsRecursive(
  node: UiTreeNode,
  path: UiNodePath,
  out: Set<UiNodePath>,
): boolean {
  const attrs = node.attributes;
  let interesting = Boolean(attrs["id"] || attrs["text"] || attrs["label"]);
  for (let i = 0; i < node.children.length; i++) {
    const childPath = path === "" ? String(i) : `${path}.${i}`;
    if (collectInterestingPathsRecursive(node.children[i]!, childPath, out)) {
      interesting = true;
    }
  }
  if (interesting) out.add(path);
  return interesting;
}

/**
 * Enumerate paths of every branch node a Collapse-All operation
 * should fold. Leaves are skipped (a leaf has nothing to collapse)
 * and the tree root itself is omitted so Collapse-All leaves the
 * top-level children visible rather than hiding everything behind
 * a single line.
 */
export function collectBranchPaths(
  tree: UiTreeNode | null,
): ReadonlySet<UiNodePath> {
  const set = new Set<UiNodePath>();
  if (!tree) return set;
  collectBranchPathsRecursive(tree, "", set);
  return set;
}

function collectBranchPathsRecursive(
  node: UiTreeNode,
  path: UiNodePath,
  out: Set<UiNodePath>,
): void {
  if (node.children.length === 0) return;
  if (path !== "") out.add(path);
  for (let i = 0; i < node.children.length; i += 1) {
    const childPath = path === "" ? String(i) : `${path}.${i}`;
    collectBranchPathsRecursive(node.children[i]!, childPath, out);
  }
}
