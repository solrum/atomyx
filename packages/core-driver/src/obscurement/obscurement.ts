import type { TreeNode } from "../tree/tree-node.js";
import { AttrKeys, Roles, getAttr } from "../tree/tree-node.js";
import { parseBounds, boundsContain, boundsCenter } from "../tree/bounds.js";

/**
 * Obscurement detection — given a target element and the tree it
 * lives in, decide whether some OTHER element is rendered on top
 * of the target's midpoint. Returns `null` when the target is
 * unobscured (safe to tap), or an `ObscurerInfo` describing the
 * blocking element when it is.
 *
 * This runs entirely on the canonical `TreeNode` shape — NO
 * platform-native types. The previous iOS-only implementation
 * in `modules/core-driver/native/ios-driver/Tests/Bridge/XCUIBridge.swift` used
 * `XCUIElementSnapshot`; that logic is now mirrored here in pure
 * TypeScript operating on the wire-normalized tree. Android gets
 * obscurement detection for free because the tree shape is the
 * same.
 *
 * Algorithm:
 *
 *   1. Pre-order DFS walk of the tree. For each node whose bounds
 *      contain the target's midpoint, update `topmost`. Because
 *      pre-order visits parents before children and earlier
 *      siblings before later ones, the LAST node seen that
 *      contains the point is the topmost in z-order — later
 *      siblings (and their children) render on top of earlier
 *      ones in UIKit / Compose / the DOM alike.
 *
 *   2. Reference-equality check: if `topmost === target`, the
 *      target IS the topmost node — not obscured.
 *
 *   3. Ancestor disambiguation: if `target` is a descendant of
 *      `topmost`, then `topmost` is just an ancestor container
 *      whose frame happens to enclose the target (which it always
 *      does — parents enclose children by construction). Not
 *      obscured. This was the Settings root false-positive bug
 *      on iOS 26: `UICollectionView` wrapping a cell wrapping a
 *      label gets returned as "topmost containing midpoint" but
 *      is an ancestor, not a blocker.
 *
 *   4. Generic container suppression: if `topmost` has role
 *      `container` / `other` AND no `id` / `label`, treat as a
 *      transparent layout wrapper, not a rendering blocker.
 *      Real modals / sheets / alerts / floating buttons always
 *      have a distinctive role OR a non-empty identifier/label.
 *
 *   5. Otherwise, return the topmost as the obscurer. Consumer
 *      can surface a structured error with enough info to either
 *      dismiss the obscuring element or tap it directly.
 */
export interface ObscurerInfo {
  readonly role: string;
  readonly id: string;
  readonly label: string;
}

/**
 * Result of obscurement analysis. `obscured=false` with no
 * additional fields means "safe to tap". `obscured=true` includes
 * the blocker's public attributes so callers can construct a
 * readable error or attempt recovery.
 */
export type ObscurementResult =
  | { readonly obscured: false }
  | { readonly obscured: true; readonly obscurer: ObscurerInfo };

/**
 * Compute obscurement for a target node within its containing
 * tree. `target` must be a node reachable from `root` — the
 * function does NOT verify this; passing an unrelated node is a
 * programming error.
 *
 * If the target's bounds attribute is missing or malformed, the
 * function returns `obscured:false` (cannot reason without
 * geometry — fail open rather than block legitimate taps).
 */
export function detectObscurement(
  root: TreeNode,
  target: TreeNode,
): ObscurementResult {
  const targetBounds = parseBounds(getAttr(target, AttrKeys.Bounds));
  if (!targetBounds) return { obscured: false };

  const { x, y } = boundsCenter(targetBounds);

  // Pre-order DFS — track the last containing node (topmost in
  // z-order by the pre-order-visits-later argument).
  let topmost: TreeNode | null = null;
  const stack: TreeNode[] = [root];
  while (stack.length > 0) {
    const node = stack.pop()!;
    const b = parseBounds(getAttr(node, AttrKeys.Bounds));
    if (b && boundsContain(b, x, y)) {
      topmost = node;
    }
    // Push children in REVERSE so pop yields them in original
    // order — maintains pre-order DFS traversal.
    for (let i = node.children.length - 1; i >= 0; i--) {
      stack.push(node.children[i]!);
    }
  }

  if (!topmost) return { obscured: false };
  if (topmost === target) return { obscured: false };

  // Ancestor check: if target is reachable from topmost's
  // subtree, topmost is an ancestor container, not an obscurer.
  if (containsNode(topmost, target)) return { obscured: false };

  // Generic container suppression.
  const role = getAttr(topmost, AttrKeys.Role) ?? "";
  const id = getAttr(topmost, AttrKeys.Id) ?? "";
  const label = getAttr(topmost, AttrKeys.Label) ?? "";
  if ((role === Roles.Container || role === Roles.Other) && id === "" && label === "") {
    return { obscured: false };
  }

  return {
    obscured: true,
    obscurer: { role, id, label },
  };
}

/**
 * Iterative subtree search — is `target` reachable from `root`
 * by walking children? Reference equality, not structural
 * equality. Used by `detectObscurement` for the ancestor check.
 */
function containsNode(root: TreeNode, target: TreeNode): boolean {
  const stack: TreeNode[] = [root];
  while (stack.length > 0) {
    const node = stack.pop()!;
    if (node === target) return true;
    for (const child of node.children) {
      stack.push(child);
    }
  }
  return false;
}
