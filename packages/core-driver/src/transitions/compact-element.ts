import type { TreeNode } from "../tree/tree-node.js";
import { AttrKeys, getAttr } from "../tree/tree-node.js";
import { parseBounds, type Bounds } from "../tree/bounds.js";

/**
 * Flat compact element used by the transition diagnostics layer.
 * Equivalent to the legacy `CompactElement` shape in
 * `src/adapters/device-controller.port.ts` — kept flat because
 * the diagnostic functions (loading detection, motion, overlay
 * analysis, diff) work on a linear element list, not on the
 * hierarchical TreeNode tree.
 *
 * New code operates on `TreeNode` at the framework boundary;
 * `treeNodeToCompactElements` bridges the two when a tool
 * needs to run diagnostics.
 */
export interface CompactElement {
  readonly elementId: string;
  readonly role: string;
  readonly label: string;
  readonly text: string;
  readonly resourceId?: string;
  readonly bounds: Bounds;
  readonly clickable: boolean;
  readonly enabled: boolean;
  /**
   * Minimal selector-shape map used by diagnostic functions to
   * key elements across pre/post samples.
   */
  readonly selector?: Record<string, string>;
}

/**
 * Flatten a `TreeNode` hierarchy into a `CompactElement` list
 * (pre-order walk, nodes with invalid bounds dropped). Used by
 * tools that need to run transition diagnostics without rewriting
 * the battle-tested pure functions for tree-shaped input.
 *
 * Nodes with no bounds attribute are skipped — they have no
 * geometry to reason about and typically represent invisible
 * semantic grouping nodes.
 */
export function treeNodeToCompactElements(root: TreeNode): CompactElement[] {
  const out: CompactElement[] = [];
  let elementCounter = 0;
  const stack: TreeNode[] = [root];
  while (stack.length > 0) {
    const node = stack.pop()!;
    for (let i = node.children.length - 1; i >= 0; i--) {
      stack.push(node.children[i]!);
    }
    const boundsStr = getAttr(node, AttrKeys.Bounds);
    const bounds = parseBounds(boundsStr);
    if (!bounds) continue;
    const resourceId = getAttr(node, AttrKeys.Id);
    const label = getAttr(node, AttrKeys.Label) ?? "";
    const text = getAttr(node, AttrKeys.Text) ?? "";
    const role = getAttr(node, AttrKeys.Role) ?? "";
    const selector: Record<string, string> = {};
    if (resourceId) selector.resourceId = resourceId;
    if (label) selector.contentDesc = label;
    if (text) selector.text = text;
    out.push({
      elementId: `tree-${elementCounter++}`,
      role,
      label,
      text,
      resourceId: resourceId || undefined,
      bounds,
      clickable: node.clickable === true,
      enabled: node.enabled !== false,
      selector: Object.keys(selector).length > 0 ? selector : undefined,
    });
  }
  return out;
}

/**
 * Shape of an already-resolved element (from `Finder.findOne`).
 * Used by `detectTargetStateChange` to compare the tap target's
 * state before and after. Shape is a subset of `CompactElement`
 * plus a `found` flag for convenience.
 */
export interface ResolvedElementState {
  readonly found: boolean;
  readonly role?: string;
  readonly enabled?: boolean;
  readonly clickable?: boolean;
  readonly bounds?: Bounds;
}
