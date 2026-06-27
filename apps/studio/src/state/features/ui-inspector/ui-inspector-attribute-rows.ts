import type { UiTreeNode } from "../../../domain/features/runtime/index.js";

export interface AttributeRow {
  readonly key: string;
  readonly value: string;
}

/**
 * State flags carried directly on `UiTreeNode` (not under
 * `attributes`). They surface in the attributes panel alongside
 * raw attributes so the user sees the full reported state in one
 * sorted list.
 */
const STATE_FLAGS = [
  "clickable",
  "enabled",
  "focused",
  "selected",
  "checked",
  "visible",
] as const satisfies readonly (keyof UiTreeNode)[];

/**
 * Build the sorted attribute table shown for the selected node.
 * Pulls every key from `node.attributes` plus any defined boolean
 * state flag, then sorts alphabetically so two consecutive selections
 * present rows in the same order. Returns `[]` for `null`.
 */
export function attributeRows(node: UiTreeNode | null): readonly AttributeRow[] {
  if (!node) return [];
  const entries: AttributeRow[] = [];
  for (const [key, value] of Object.entries(node.attributes)) {
    entries.push({ key, value });
  }
  for (const flag of STATE_FLAGS) {
    const v = node[flag];
    if (v !== undefined) {
      entries.push({ key: flag, value: String(v) });
    }
  }
  entries.sort((a, b) => a.key.localeCompare(b.key));
  return entries;
}
