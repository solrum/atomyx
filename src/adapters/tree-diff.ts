/**
 * Compute structural diff between two UI tree snapshots.
 *
 * Used by:
 *   - Mode C explorer: detect "did the screen change?" after an action
 *   - report_bug: capture before/after for context
 *   - get_tree_diff MCP tool
 *
 * Strategy: flatten both trees by stable identifier (resourceId+className+text),
 * compare sets, return added/removed/changed.
 */

import type { RawElement } from "./device-controller.port.js";

export interface TreeDiff {
  added: NodeKey[];
  removed: NodeKey[];
  changed: ChangedNode[];
  unchanged: number;
}

export interface NodeKey {
  resourceId?: string;
  className?: string;
  text?: string;
  contentDesc?: string;
}

export interface ChangedNode {
  key: NodeKey;
  before: Partial<RawElement>;
  after: Partial<RawElement>;
}

function nodeKey(n: RawElement): string {
  return [
    n.resourceId ?? "",
    n.className ?? "",
    n.text ?? "",
    n.contentDesc ?? "",
  ].join("|");
}

function flatten(root: RawElement): Map<string, RawElement> {
  const map = new Map<string, RawElement>();
  function walk(n: RawElement) {
    map.set(nodeKey(n), n);
    for (const c of n.children ?? []) walk(c);
  }
  walk(root);
  return map;
}

function pickKey(n: RawElement): NodeKey {
  return {
    resourceId: n.resourceId,
    className: n.className,
    text: n.text,
    contentDesc: n.contentDesc,
  };
}

function pickFields(n: RawElement): Partial<RawElement> {
  return {
    text: n.text,
    contentDesc: n.contentDesc,
    clickable: n.clickable,
    enabled: n.enabled,
    bounds: n.bounds,
  };
}

function fieldsEqual(a: RawElement, b: RawElement): boolean {
  return (
    a.text === b.text &&
    a.contentDesc === b.contentDesc &&
    a.clickable === b.clickable &&
    a.enabled === b.enabled
  );
}

export function diffTrees(before: RawElement, after: RawElement): TreeDiff {
  const beforeMap = flatten(before);
  const afterMap = flatten(after);

  const added: NodeKey[] = [];
  const removed: NodeKey[] = [];
  const changed: ChangedNode[] = [];
  let unchanged = 0;

  for (const [key, node] of afterMap) {
    if (!beforeMap.has(key)) {
      added.push(pickKey(node));
    } else {
      const old = beforeMap.get(key)!;
      if (!fieldsEqual(old, node)) {
        changed.push({ key: pickKey(node), before: pickFields(old), after: pickFields(node) });
      } else {
        unchanged++;
      }
    }
  }

  for (const [key, node] of beforeMap) {
    if (!afterMap.has(key)) {
      removed.push(pickKey(node));
    }
  }

  return { added, removed, changed, unchanged };
}

/**
 * Cheap "did anything change?" check that doesn't allocate diff arrays.
 */
export function treesEqual(a: RawElement, b: RawElement): boolean {
  const ma = flatten(a);
  const mb = flatten(b);
  if (ma.size !== mb.size) return false;
  for (const [key, node] of ma) {
    const other = mb.get(key);
    if (!other) return false;
    if (!fieldsEqual(node, other)) return false;
  }
  return true;
}

/**
 * Walk tree, collect text + contentDesc into a single haystack for
 * `must_contain` / `must_not_contain` checks.
 */
export function flattenText(root: RawElement): string[] {
  const out: string[] = [];
  function walk(n: RawElement) {
    if (n.text) out.push(n.text);
    if (n.contentDesc) out.push(n.contentDesc);
    for (const c of n.children ?? []) walk(c);
  }
  walk(root);
  return out;
}
