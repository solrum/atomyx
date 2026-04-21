import { AttrKeys, Roles, getAttr, type TreeNode } from "../tree/tree-node.js";
import { walk, type TreeCursor } from "../tree/tree-cursor.js";
import { parseBounds, type Bounds } from "../tree/bounds.js";

/**
 * Observable state derived from a `TreeNode` snapshot. These functions
 * are the host-side alternative to adding `getFocusedNode` /
 * `getKeyboardState` methods on the Driver port — per the port's own
 * rule (`driver.port.ts:11-13`): anything implementable from existing
 * methods does NOT belong on the interface.
 *
 * `hierarchy()` returns a fresh snapshot per call (contract) and these
 * queries are pure functions of that snapshot. Callers wanting to
 * react to state changes compose them with the wait primitives in
 * `packages/driver/src/waits/`.
 *
 * Cross-platform semantics:
 *
 *   - "Focused" means INPUT focus — the element that would receive
 *     keystrokes right now. Android: `AccessibilityNodeInfo.isFocused`.
 *     iOS: `XCUIElementSnapshot.hasKeyboardFocus`. Normalized onto
 *     `TreeNode.focused` by the driver adapters.
 *
 *   - "Keyboard" means the on-screen system IME. Detected by either
 *     `role === "keyboard"` (iOS, which has a dedicated elementType)
 *     or `ext:isIme === "true"` (Android, where IME windows come
 *     through the accessibility surface without a role marker).
 */

export interface KeyboardState {
  readonly visible: boolean;
  readonly bounds?: Bounds;
}

export function findFocusedNode(tree: TreeNode): TreeCursor | null {
  for (const c of walk(tree)) {
    if (c.node.focused === true) return c;
  }
  return null;
}

export function findKeyboardNode(tree: TreeNode): TreeCursor | null {
  for (const c of walk(tree)) {
    if (isKeyboardNode(c.node)) return c;
  }
  return null;
}

export function readKeyboardState(tree: TreeNode): KeyboardState {
  const kb = findKeyboardNode(tree);
  if (!kb) return { visible: false };
  const bounds = parseBounds(getAttr(kb.node, AttrKeys.Bounds));
  return bounds ? { visible: true, bounds } : { visible: true };
}

function isKeyboardNode(node: TreeNode): boolean {
  if (getAttr(node, AttrKeys.Role) === Roles.Keyboard) return true;
  if (node.attributes["ext:isIme"] === "true") return true;
  return false;
}
