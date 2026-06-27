import type { TreeNode } from "../tree/tree-node.js";
import { AttrKeys } from "../tree/tree-node.js";

/**
 * Pixel size of the bucket each bounds coordinate is rounded into
 * before hashing. Picked larger than the typical iOS UIScrollView
 * rubber-band settle drift (usually < 30 px) so a bounce on a
 * non-scrollable container does not appear as a content change.
 * Real list scrolls move items by at least one row height (≥ 40 px
 * on common layouts), so legitimate scrolling crosses bucket
 * boundaries reliably.
 */
const BOUNDS_BUCKET_PX = 50;

/**
 * Hash the set of visible leaf nodes in a UI tree.
 *
 * A leaf is any node with no children (or an empty children array).
 * Each leaf contributes a string combining its text, label, and
 * bucketed bounds attributes. Leaf strings are sorted before
 * hashing so sibling order does not affect the result — only the
 * SET of visible leaves matters.
 *
 * Bounds are quantized into BOUNDS_BUCKET_PX cells before hashing
 * so sub-bucket drift (iOS rubber-band bounce settling back to
 * "near" original position) does not flip the hash. Two consecutive
 * snapshots of the same content land in the same buckets even when
 * the platform reports bounds off by a few pixels.
 *
 * Returns a djb2 hex string. Callers compare two consecutive hashes
 * to detect scroll-boundary saturation: equal hashes mean the tree
 * did not change after a swipe (boundary hit or non-scrollable
 * container), so further swipes in that direction are pointless.
 */
export function hashVisibleLeaves(tree: TreeNode): string {
  const parts: string[] = [];
  collectLeaves(tree, parts);
  parts.sort();
  return djb2(parts.join("\x00"));
}

/**
 * Count leaf nodes in the tree.
 *
 * Used for the viewport pre-check: a tree with very few leaves that
 * also occupies little vertical space is likely a lazy-loaded or
 * non-scrollable container; the swipe budget is capped early.
 */
export function countLeaves(tree: TreeNode): number {
  if (tree.children.length === 0) return 1;
  let count = 0;
  for (const child of tree.children) {
    count += countLeaves(child);
  }
  return count;
}

/**
 * Return the maximum `bottom` value (in the "left,top,right,bottom"
 * bounds attribute) across all leaf nodes.
 *
 * Used alongside `countLeaves` to detect a thin tree: few leaves
 * that sit high on screen indicate a container that hasn't loaded
 * much content yet or is not scrollable. Returns 0 when no leaf
 * carries a parseable bounds attribute.
 */
export function maxLeafBoundsBottom(tree: TreeNode): number {
  let max = 0;
  visitLeaves(tree, (node) => {
    const bounds = node.attributes[AttrKeys.Bounds];
    if (!bounds) return;
    const parts = bounds.split(",");
    // bounds format: "left,top,right,bottom"
    const bottom = Number(parts[3]);
    if (!Number.isNaN(bottom) && bottom > max) max = bottom;
  });
  return max;
}

// ── Internals ─────────────────────────────────────────────────────

function collectLeaves(node: TreeNode, out: string[]): void {
  if (node.children.length === 0) {
    const text = node.attributes[AttrKeys.Text] ?? "";
    const label = node.attributes[AttrKeys.Label] ?? "";
    const bounds = node.attributes[AttrKeys.Bounds] ?? "";
    out.push(`${text}|${label}|${bucketBounds(bounds)}`);
    return;
  }
  for (const child of node.children) {
    collectLeaves(child, out);
  }
}

function bucketBounds(bounds: string): string {
  if (!bounds) return "";
  const parts = bounds.split(",");
  if (parts.length !== 4) return bounds;
  const out: string[] = new Array(4);
  for (let i = 0; i < 4; i++) {
    const n = Number(parts[i]);
    out[i] = Number.isNaN(n) ? parts[i] : String(Math.floor(n / BOUNDS_BUCKET_PX));
  }
  return out.join(",");
}

function visitLeaves(node: TreeNode, cb: (n: TreeNode) => void): void {
  if (node.children.length === 0) {
    cb(node);
    return;
  }
  for (const child of node.children) {
    visitLeaves(child, cb);
  }
}

/**
 * djb2 hash — start=5381, per char: hash = ((hash << 5) + hash) ^ charCode.
 * Returns unsigned result as lowercase hex string.
 */
function djb2(s: string): string {
  let hash = 5381;
  for (let i = 0; i < s.length; i++) {
    hash = ((hash << 5) + hash) ^ s.charCodeAt(i);
    hash = hash >>> 0; // keep unsigned 32-bit
  }
  return hash.toString(16);
}
