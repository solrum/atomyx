import { AttrKeys, getAttr } from "../tree/tree-node.js";
import {
  ancestorsOf,
  descendantsOf,
  flatten,
  type TreeCursor,
} from "../tree/tree-cursor.js";
import { parseBounds, boundsCenter } from "../tree/bounds.js";
import type { TreeNode } from "../tree/tree-node.js";

/**
 * Core query primitive. An `ElementFilter` is a pure function from
 * a list of cursors to a (possibly smaller) list of cursors. Filters
 * are composed via `intersect` / `union` / `not` to express arbitrary
 * AND / OR / NOT selector logic without a strategy-class hierarchy.
 *
 * Design rationale vs. the earlier strategy-class approach:
 *
 *   - Strategy classes force fallback-ordered resolution ("try
 *     resourceId first, then text, then hint"). That's only one
 *     possible policy — it can't express "match BOTH resourceId=X
 *     AND text=Y" or "match any element WHERE parent.text=Header".
 *
 *   - Functions compose. `intersect(idMatches(x), textMatches(y))`
 *     is strictly more expressive than "try id, fall back to text".
 *     Priority-broadening (the agent ergonomic we want) is a
 *     HIGHER-LEVEL policy built on top of these primitives — see
 *     `selectors/priority-broadening.ts` (not in this file).
 *
 *   - Functions are pure and stateless. Unit testing a filter is
 *     calling it with a fixture cursor list and asserting output;
 *     no mocking, no setup.
 *
 * All filters take and return `TreeCursor[]` rather than
 * `TreeNode[]` so spatial filters (`below`, `hasParent`, ...) have
 * access to the parent chain without forcing `parent` onto the
 * wire shape.
 */
export type ElementFilter = (cursors: readonly TreeCursor[]) => TreeCursor[];

/**
 * Entry point — produce a filterable cursor list from a fresh tree.
 * Equivalent to `flatten(root)` but reads more intent-fully in
 * caller code: `query(tree).then(intersect(...))`.
 */
export function fromTree(root: TreeNode): TreeCursor[] {
  return flatten(root);
}

// ─── Attribute filters ───────────────────────────────────────────

function matches(value: string | undefined, pattern: string | RegExp): boolean {
  if (value === undefined) return false;
  if (typeof pattern === "string") return value === pattern;
  return pattern.test(value);
}

function byAttr(key: string, pattern: string | RegExp): ElementFilter {
  return (cursors) => cursors.filter((c) => matches(getAttr(c.node, key), pattern));
}

export function idMatches(pattern: string | RegExp): ElementFilter {
  return byAttr(AttrKeys.Id, pattern);
}

export function textMatches(pattern: string | RegExp): ElementFilter {
  return byAttr(AttrKeys.Text, pattern);
}

export function labelMatches(pattern: string | RegExp): ElementFilter {
  return byAttr(AttrKeys.Label, pattern);
}

export function hintMatches(pattern: string | RegExp): ElementFilter {
  return byAttr(AttrKeys.Hint, pattern);
}

export function valueMatches(pattern: string | RegExp): ElementFilter {
  return byAttr(AttrKeys.Value, pattern);
}

export function roleIs(role: string): ElementFilter {
  return byAttr(AttrKeys.Role, role);
}

export function classMatches(pattern: string | RegExp): ElementFilter {
  return byAttr(AttrKeys.Class, pattern);
}

/**
 * Generic attribute filter — escape hatch for keys not covered by
 * the named helpers above, including `ext:*` driver-specific keys.
 * Cross-platform callers should prefer the typed helpers.
 */
export function attributeMatches(
  key: string,
  pattern: string | RegExp,
): ElementFilter {
  return byAttr(key, pattern);
}

// ─── State filters ───────────────────────────────────────────────

export function isEnabled(): ElementFilter {
  return (cursors) => cursors.filter((c) => c.node.enabled === true);
}

export function isClickable(): ElementFilter {
  return (cursors) => cursors.filter((c) => c.node.clickable === true);
}

export function isFocused(): ElementFilter {
  return (cursors) => cursors.filter((c) => c.node.focused === true);
}

export function isChecked(): ElementFilter {
  return (cursors) => cursors.filter((c) => c.node.checked === true);
}

export function isSelected(): ElementFilter {
  return (cursors) => cursors.filter((c) => c.node.selected === true);
}

// ─── Spatial filters ─────────────────────────────────────────────

function centerOfCursor(c: TreeCursor): { x: number; y: number } | null {
  const b = parseBounds(getAttr(c.node, AttrKeys.Bounds));
  if (!b) return null;
  return boundsCenter(b);
}

/**
 * Keep cursors whose center lies strictly below the topmost
 * anchor match. "Below" = greater Y (screen space). Resolves
 * `anchor` against the SAME cursor list the outer filter receives,
 * so composition works inside any subtree.
 */
export function below(anchor: ElementFilter): ElementFilter {
  return (cursors) => {
    const anchors = anchor(cursors);
    if (anchors.length === 0) return [];
    const anchorY = Math.min(
      ...anchors
        .map((a) => centerOfCursor(a)?.y)
        .filter((y): y is number => y !== undefined),
    );
    if (!Number.isFinite(anchorY)) return [];
    return cursors.filter((c) => {
      const cy = centerOfCursor(c)?.y;
      return cy !== undefined && cy > anchorY;
    });
  };
}

export function above(anchor: ElementFilter): ElementFilter {
  return (cursors) => {
    const anchors = anchor(cursors);
    if (anchors.length === 0) return [];
    const anchorY = Math.max(
      ...anchors
        .map((a) => centerOfCursor(a)?.y)
        .filter((y): y is number => y !== undefined),
    );
    if (!Number.isFinite(anchorY)) return [];
    return cursors.filter((c) => {
      const cy = centerOfCursor(c)?.y;
      return cy !== undefined && cy < anchorY;
    });
  };
}

export function leftOf(anchor: ElementFilter): ElementFilter {
  return (cursors) => {
    const anchors = anchor(cursors);
    if (anchors.length === 0) return [];
    const anchorX = Math.max(
      ...anchors
        .map((a) => centerOfCursor(a)?.x)
        .filter((x): x is number => x !== undefined),
    );
    if (!Number.isFinite(anchorX)) return [];
    return cursors.filter((c) => {
      const cx = centerOfCursor(c)?.x;
      return cx !== undefined && cx < anchorX;
    });
  };
}

export function rightOf(anchor: ElementFilter): ElementFilter {
  return (cursors) => {
    const anchors = anchor(cursors);
    if (anchors.length === 0) return [];
    const anchorX = Math.min(
      ...anchors
        .map((a) => centerOfCursor(a)?.x)
        .filter((x): x is number => x !== undefined),
    );
    if (!Number.isFinite(anchorX)) return [];
    return cursors.filter((c) => {
      const cx = centerOfCursor(c)?.x;
      return cx !== undefined && cx > anchorX;
    });
  };
}

// ─── Structural filters ──────────────────────────────────────────

/**
 * Keep cursors whose direct parent matches `filter`. `filter` is
 * evaluated against each candidate's parent cursor in isolation
 * (wrapped as a single-element list) — typical use is
 * `hasParent(roleIs("cell"))`, not anchor-style selection.
 */
export function hasParent(filter: ElementFilter): ElementFilter {
  return (cursors) =>
    cursors.filter((c) => {
      if (!c.parent) return false;
      return filter([c.parent]).length > 0;
    });
}

/**
 * Keep cursors with at least one descendant matching `filter`.
 * Walks the subtree rooted at each candidate.
 */
export function hasDescendant(filter: ElementFilter): ElementFilter {
  return (cursors) =>
    cursors.filter((c) => filter(descendantsOf(c)).length > 0);
}

/**
 * Keep cursors that have at least one ancestor matching `filter`.
 * Symmetric to `hasDescendant` but walking upward. Useful for
 * "button inside a specific dialog" style queries.
 */
export function hasAncestor(filter: ElementFilter): ElementFilter {
  return (cursors) =>
    cursors.filter((c) => filter(ancestorsOf(c)).length > 0);
}

// ─── Composition ─────────────────────────────────────────────────

/**
 * AND — keep cursors that match every filter. Short-circuits when
 * the intermediate result becomes empty.
 */
export function intersect(...filters: ElementFilter[]): ElementFilter {
  return (cursors) => {
    let result: readonly TreeCursor[] = cursors;
    for (const f of filters) {
      result = f(result);
      if (result.length === 0) return [];
    }
    return [...result];
  };
}

/**
 * OR — return cursors matching any of the filters. Preserves the
 * original order of `cursors`, deduplicating on cursor identity.
 */
export function union(...filters: ElementFilter[]): ElementFilter {
  return (cursors) => {
    const set = new Set<TreeCursor>();
    for (const f of filters) {
      for (const c of f(cursors)) set.add(c);
    }
    return cursors.filter((c) => set.has(c));
  };
}

/**
 * NOT — return cursors that do NOT match the filter. The universe
 * is the input list: `not(f)(cursors) = cursors \ f(cursors)`.
 */
export function not(filter: ElementFilter): ElementFilter {
  return (cursors) => {
    const excluded = new Set(filter(cursors));
    return cursors.filter((c) => !excluded.has(c));
  };
}

// ─── Ordering / selection ────────────────────────────────────────

export function first(filter: ElementFilter): ElementFilter {
  return (cursors) => {
    const r = filter(cursors);
    return r.length > 0 ? [r[0]!] : [];
  };
}

export function nth(filter: ElementFilter, index: number): ElementFilter {
  return (cursors) => {
    const r = filter(cursors);
    if (index < 0 || index >= r.length) return [];
    return [r[index]!];
  };
}

/**
 * Sort cursors by their geometric top-left corner — top first,
 * then left-to-right within the same row. Cursors without valid
 * bounds are dropped (can't sort spatially without geometry).
 */
export function sortByTopLeft(): ElementFilter {
  return (cursors) => {
    return [...cursors]
      .map((c) => ({ c, b: parseBounds(getAttr(c.node, AttrKeys.Bounds)) }))
      .filter((e): e is { c: TreeCursor; b: NonNullable<typeof e.b> } => e.b !== null)
      .sort((a, b) => {
        if (a.b.top !== b.b.top) return a.b.top - b.b.top;
        return a.b.left - b.b.left;
      })
      .map((e) => e.c);
  };
}
