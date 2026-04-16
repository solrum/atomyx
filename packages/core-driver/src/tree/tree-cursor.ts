import type { TreeNode } from "./tree-node.js";

/**
 * Navigation wrapper around a {@link TreeNode} that preserves the
 * parent pointer during tree walks. Used by filter composition so
 * spatial / structural filters (`hasParent`, `below`, `hasDescendant`)
 * can climb back up without forcing `parent` onto the canonical
 * `TreeNode` shape itself.
 *
 * Why a separate cursor instead of adding `parent` to TreeNode:
 *
 *   1. TreeNode is the WIRE shape. Drivers deserialize JSON straight
 *      into it — adding a `parent` pointer would either require a
 *      post-process step on every tree (cost + footgun if skipped)
 *      or force circular JSON.
 *
 *   2. Immutability. `parent` on TreeNode would make the data model
 *      circular, which defeats `Readonly<>` and breaks structural
 *      equality comparisons.
 *
 *   3. Filter composition is the ONLY place that needs parent
 *      traversal. Keeping the concern isolated in a cursor type
 *      means the rest of the codebase operates on the simpler
 *      TreeNode.
 *
 * Cursors are produced by `walk()` below — a single pre-order DFS
 * that yields one cursor per node, with `parent` filled in lazily.
 * Filters accept `TreeCursor[]` as input and return `TreeCursor[]`
 * as output; the final stage (e.g. `centerOf`, `idOf`) reads
 * `cursor.node.attributes` to get back to canonical data.
 */
export interface TreeCursor {
  readonly node: TreeNode;
  readonly parent: TreeCursor | null;
  readonly depth: number;
  /** Zero-based index among the parent's children. Root is 0. */
  readonly childIndex: number;
}

/**
 * Walk a tree in pre-order (parent before children, children in
 * original order) yielding a cursor for every node. Root is
 * yielded with `parent=null`, `depth=0`, `childIndex=0`.
 *
 * Iterative (explicit stack) rather than recursive to avoid blowing
 * the JS call stack on deep trees — iOS XCUIElement snapshots can
 * exceed 1000 nodes on complex screens, which is safe but close
 * enough to typical default limits that recursion is risky.
 */
export function walk(root: TreeNode): TreeCursor[] {
  const result: TreeCursor[] = [];
  const rootCursor: TreeCursor = {
    node: root,
    parent: null,
    depth: 0,
    childIndex: 0,
  };
  const stack: TreeCursor[] = [rootCursor];
  while (stack.length > 0) {
    const cursor = stack.pop()!;
    result.push(cursor);
    // Push children in REVERSE order so popLast() yields them in
    // original document order (pre-order DFS).
    const children = cursor.node.children;
    for (let i = children.length - 1; i >= 0; i--) {
      stack.push({
        node: children[i]!,
        parent: cursor,
        depth: cursor.depth + 1,
        childIndex: i,
      });
    }
  }
  return result;
}

/**
 * Synonym for `walk(root)` with "flatten" naming — used by filter
 * entry points that conceptually want "the list of cursors" rather
 * than "a walk".
 */
export function flatten(root: TreeNode): TreeCursor[] {
  return walk(root);
}

/**
 * Collect every ancestor of a cursor, from immediate parent up to
 * root. Result is ordered parent-first. Empty list for the root
 * cursor.
 */
export function ancestorsOf(cursor: TreeCursor): TreeCursor[] {
  const result: TreeCursor[] = [];
  let p = cursor.parent;
  while (p) {
    result.push(p);
    p = p.parent;
  }
  return result;
}

/**
 * Walk the parent chain of a cursor up to the root and return the
 * root's `node`. Useful when a consumer holds a cursor into a tree
 * and needs the tree root without fetching a fresh `hierarchy()` —
 * e.g. Orchestra's obscurement pass wants to detect blockers in
 * the SAME tree the cursor points into, so that
 * `detectObscurement`'s reference-identity checks work.
 *
 * Cheap — O(depth), no tree walk, no allocation.
 */
export function rootNodeOf(cursor: TreeCursor): TreeNode {
  let c: TreeCursor = cursor;
  while (c.parent) c = c.parent;
  return c.node;
}

/**
 * Collect every descendant of a cursor (NOT including itself),
 * in pre-order. Produces fresh `TreeCursor` instances with this
 * cursor as their common ancestor chain root — parents are NOT
 * back-linked to the cursor's own parent chain, only to `cursor`
 * itself. Intended for `hasDescendant` filter use; callers that
 * need the original parent chain should use `walk()` from the
 * tree root and filter by `ancestorsOf()`.
 */
export function descendantsOf(cursor: TreeCursor): TreeCursor[] {
  const result: TreeCursor[] = [];
  const stack: TreeCursor[] = [cursor];
  while (stack.length > 0) {
    const c = stack.pop()!;
    if (c !== cursor) result.push(c);
    const children = c.node.children;
    for (let i = children.length - 1; i >= 0; i--) {
      stack.push({
        node: children[i]!,
        parent: c,
        depth: c.depth + 1,
        childIndex: i,
      });
    }
  }
  return result;
}
