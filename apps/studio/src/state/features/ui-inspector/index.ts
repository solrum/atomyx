import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  UiInspectorApi,
  UiInspectorSnapshot,
  UiNodePath,
  UiTreeNode,
} from "./ui-inspector.contract.js";
import {
  createZustandUiInspector,
  type UiInspectorDeps,
} from "./ui-inspector.zustand.js";

export type { UiInspectorApi, UiInspectorSnapshot, UiNodePath, UiTreeNode };

export {
  collectBranchPaths,
  collectInterestingPaths,
  isInformative,
} from "./tree-filter.js";
export { summarize, truncate } from "./tree-display.js";
export { attributeRows, type AttributeRow } from "./attribute-rows.js";

export const UI_INSPECTOR_KEY = "ui-inspector";

export function createUiInspector(deps: UiInspectorDeps): UiInspectorApi {
  return createZustandUiInspector(deps);
}

export function useUiInspector(): UiInspectorSnapshot &
  Pick<
    UiInspectorApi,
    | "refresh"
    | "select"
    | "clear"
    | "setShowRaw"
    | "setAutoRefreshEnabled"
    | "setAutoRefreshInterval"
    | "notifyInteraction"
  > {
  const api = getFeature<UiInspectorApi>(UI_INSPECTOR_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    refresh: api.refresh,
    select: api.select,
    clear: api.clear,
    setShowRaw: api.setShowRaw,
    setAutoRefreshEnabled: api.setAutoRefreshEnabled,
    setAutoRefreshInterval: api.setAutoRefreshInterval,
    notifyInteraction: api.notifyInteraction,
  };
}

/**
 * Walk the path from root to the node it addresses. Returns `null`
 * if any segment is out-of-bounds — callers use that to clear a
 * stale selection after the tree shape changes.
 */
export function resolveUiNode(
  root: UiTreeNode | null,
  path: UiNodePath | null,
): UiTreeNode | null {
  if (!root || path === null) return null;
  if (path === "") return root;
  let node: UiTreeNode = root;
  for (const segment of path.split(".")) {
    const index = Number(segment);
    if (!Number.isInteger(index) || index < 0) return null;
    const child = node.children[index];
    if (!child) return null;
    node = child;
  }
  return node;
}

/**
 * Estimate the coordinate space the tree's `bounds` attributes
 * live in. Drivers report bounds in device-native pixels, which
 * do NOT match the mirror video frame when scrcpy or simctl
 * downscales. Consumers drawing overlays on the mirror normalise
 * by `videoDim / extent` before applying the canvas layout.
 *
 * Strategy: prefer the root node's own bounds (that is typically
 * the full-screen window decor view); fall back to the maximum
 * right/bottom observed across the tree. Returns `null` when no
 * node in the tree carries a parseable `bounds` attribute.
 */
export function computeTreeExtent(
  root: UiTreeNode | null,
): { readonly width: number; readonly height: number } | null {
  if (!root) return null;
  const rootBounds = parseRawBounds(root.attributes["bounds"]);
  if (rootBounds) {
    return { width: rootBounds.right, height: rootBounds.bottom };
  }
  let maxR = 0;
  let maxB = 0;
  const stack: UiTreeNode[] = [root];
  while (stack.length > 0) {
    const node = stack.pop()!;
    const b = parseRawBounds(node.attributes["bounds"]);
    if (b) {
      if (b.right > maxR) maxR = b.right;
      if (b.bottom > maxB) maxB = b.bottom;
    }
    for (const child of node.children) stack.push(child);
  }
  if (maxR === 0 || maxB === 0) return null;
  return { width: maxR, height: maxB };
}

function parseRawBounds(
  raw: string | undefined,
): { right: number; bottom: number } | null {
  if (!raw) return null;
  const parts = raw.split(",");
  if (parts.length !== 4) return null;
  const nums = parts.map((p) => Number(p.trim()));
  if (nums.some((n) => !Number.isFinite(n))) return null;
  const [, , r, b] = nums as [number, number, number, number];
  if (r <= 0 || b <= 0) return null;
  return { right: r, bottom: b };
}
