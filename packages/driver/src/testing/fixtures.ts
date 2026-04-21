import { AttrKeys, Roles } from "../tree/tree-node.js";
import type { TreeNode } from "../tree/tree-node.js";

/**
 * Small fixture builders for tests that need realistic-looking
 * trees without hand-writing attribute maps each time.
 */

export interface NodeOptions {
  id?: string;
  text?: string;
  label?: string;
  hint?: string;
  value?: string;
  role?: string;
  bounds?: string;
  enabled?: boolean;
  clickable?: boolean;
  focused?: boolean;
  checked?: boolean;
  selected?: boolean;
  children?: TreeNode[];
}

export function node(opts: NodeOptions): TreeNode {
  const attributes: Record<string, string> = {};
  if (opts.id !== undefined) attributes[AttrKeys.Id] = opts.id;
  if (opts.text !== undefined) attributes[AttrKeys.Text] = opts.text;
  if (opts.label !== undefined) attributes[AttrKeys.Label] = opts.label;
  if (opts.hint !== undefined) attributes[AttrKeys.Hint] = opts.hint;
  if (opts.value !== undefined) attributes[AttrKeys.Value] = opts.value;
  if (opts.role !== undefined) attributes[AttrKeys.Role] = opts.role;
  if (opts.bounds !== undefined) attributes[AttrKeys.Bounds] = opts.bounds;
  return {
    attributes,
    children: opts.children ?? [],
    enabled: opts.enabled,
    clickable: opts.clickable,
    focused: opts.focused,
    checked: opts.checked,
    selected: opts.selected,
  };
}

/**
 * A minimal Settings-like list. Use when testing scroll-search
 * and positional scroll-into-view.
 *
 *   root (container, 0,0,430,932)
 *   ├── general  (cell, id=general,       bounds y=100)
 *   ├── display  (cell, id=display,       bounds y=200)
 *   ├── sounds   (cell, id=sounds,        bounds y=300)
 *   └── about    (cell, id=about,         bounds y=400)
 */
export function settingsLikeTree(): TreeNode {
  return node({
    role: Roles.Container,
    bounds: "0,0,430,932",
    children: [
      node({ role: Roles.Cell, id: "general", text: "General", bounds: "0,80,430,120", enabled: true, clickable: true }),
      node({ role: Roles.Cell, id: "display", text: "Display", bounds: "0,180,430,220", enabled: true, clickable: true }),
      node({ role: Roles.Cell, id: "sounds", text: "Sounds", bounds: "0,280,430,320", enabled: true, clickable: true }),
      node({ role: Roles.Cell, id: "about", text: "About", bounds: "0,380,430,420", enabled: true, clickable: true }),
    ],
  });
}

/**
 * Target element wrapped inside a UICollectionView-like
 * hierarchy. Used to verify the ancestor-disambiguation path
 * in obscurement detection.
 *
 *   root (container, empty id/label)
 *   └── collection (other, empty id/label — the "ancestor trap")
 *       └── cell (cell, id=target, bounds 100,200,300,260)
 */
export function ancestorTrapTree(): TreeNode {
  const target = node({
    role: Roles.Cell,
    id: "target",
    bounds: "100,200,300,260",
    enabled: true,
    clickable: true,
  });
  const collection = node({
    role: Roles.Other,
    bounds: "0,0,430,932",
    children: [target],
  });
  const root = node({
    role: Roles.Container,
    bounds: "0,0,430,932",
    children: [collection],
  });
  return root;
}

/**
 * Target covered by a real modal sheet. Used to verify
 * obscurement detection flags genuine blockers.
 *
 *   root
 *   ├── cell (id=target, bounds 100,200,300,260)
 *   └── sheet (dialog, id=confirm-sheet, bounds 0,100,430,600)
 */
export function modalObscuredTree(): { root: TreeNode; target: TreeNode; sheet: TreeNode } {
  const target = node({
    role: Roles.Cell,
    id: "target",
    bounds: "100,200,300,260",
  });
  const sheet = node({
    role: Roles.Dialog,
    id: "confirm-sheet",
    label: "Confirm",
    bounds: "0,100,430,600",
  });
  const root = node({
    role: Roles.Container,
    bounds: "0,0,430,932",
    children: [target, sheet],
  });
  return { root, target, sheet };
}
