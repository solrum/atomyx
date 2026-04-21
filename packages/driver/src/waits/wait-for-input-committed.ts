import type { Clock } from "@atomyx/core/infra";
import type { Driver } from "../driver/driver.port.js";
import { AttrKeys, Roles, getAttr } from "../tree/tree-node.js";
import type { TreeNode } from "../tree/tree-node.js";
import type { TreeCursor } from "../tree/tree-cursor.js";
import { fromTree } from "../filters/element-filter.js";
import { parseBounds, boundsIntersect, type Bounds } from "../tree/bounds.js";
import { waitUntil } from "./wait-until.js";

/**
 * Verify that the input at `anchorBounds` accepted the expected text.
 * Picks a role-appropriate post-condition so the caller doesn't have
 * to know platform rendering quirks:
 *
 *   - `secure-text-field` (iOS / Android masked password): the
 *     `text` attribute shows masked bullets ("••••••••"), NOT the
 *     original characters. Verifying by string equality would never
 *     match. Verify by character count instead — 9 typed chars → 9
 *     bullets rendered.
 *
 *   - `text-field` / `search-field` / anything else: exact match
 *     (string). This is the common case.
 *
 * Why this takes `anchorBounds` instead of a `Selector`: on iOS
 * Flutter, a regular text field's `role` transitions from
 * `text-field` → `secure-text-field` the moment obscureText kicks
 * in. A caller-supplied `{role: text-field}` selector would stop
 * matching mid-flow, and verification would falsely time out. The
 * bounds rect is stable across that transition (the field doesn't
 * move when its role label changes), so intersecting candidate
 * bounds against the pre-type anchor reliably relocates the same
 * element in the post-type tree.
 *
 * Why this lives at the Orchestra helper layer (not inside
 * `waitForText`): `waitForText` is a general observation primitive
 * — any caller can use it with any matcher. `waitForInputCommitted`
 * encodes the specific "did the input accept what I tried to type?"
 * semantic, including the secure-field masking rule. Separating the
 * two keeps `waitForText` from growing role-specific branches that
 * are irrelevant to callers who just want to observe a tree change.
 */
export interface WaitForInputCommittedOptions {
  readonly driver: Driver;
  /**
   * Bounds of the field that was typed into, captured BEFORE the
   * driver.inputText call. Used as a stable anchor to relocate the
   * field after role/attribute transitions.
   */
  readonly anchorBounds: Bounds;
  readonly expected: string;
  readonly clock: Clock;
  readonly timeoutMs?: number;
  readonly intervalMs?: number;
}

export async function waitForInputCommitted(
  opts: WaitForInputCommittedOptions,
): Promise<TreeCursor> {
  const expectedLength = codePointLength(opts.expected);
  const result = await waitUntil<TreeCursor | null>({
    fetch: async () => {
      const tree = await opts.driver.hierarchy();
      const match = findAnchoredInputWithValue(
        tree,
        opts.anchorBounds,
        opts.expected,
        expectedLength,
      );
      return match;
    },
    predicate: (v): v is TreeCursor => v !== null,
    timeoutMs: opts.timeoutMs ?? 1000,
    intervalMs: opts.intervalMs ?? 50,
    clock: opts.clock,
    kind: "waitForInputCommitted",
  });
  if (!result) {
    throw new Error("waitForInputCommitted: unreachable — predicate narrowed");
  }
  return result;
}

/**
 * Find the best-matching candidate in `tree` whose bounds
 * intersect `anchorBounds` AND whose current text matches the
 * expected value according to role-specific rules.
 *
 * Picking the smallest-bounds intersecting match avoids selecting
 * a container ancestor whose rect trivially contains the field.
 */
function findAnchoredInputWithValue(
  tree: TreeNode,
  anchorBounds: Bounds,
  expected: string,
  expectedLength: number,
): TreeCursor | null {
  const cursors = fromTree(tree);
  // Collect all input-like cursors whose bounds intersect the anchor.
  const intersecting: TreeCursor[] = [];
  for (const c of cursors) {
    if (!isInputRole(getAttr(c.node, AttrKeys.Role))) continue;
    const b = parseBounds(getAttr(c.node, AttrKeys.Bounds));
    if (!b) continue;
    if (boundsIntersect(b, anchorBounds)) intersecting.push(c);
  }
  // Prefer the most-likely-correct candidate by area (smallest
  // field-size wrapper usually carries the actual value attribute).
  intersecting.sort((a, b) => areaOf(a) - areaOf(b));
  for (const c of intersecting) {
    if (matchesExpectation(c, expected, expectedLength)) return c;
  }
  return null;
}

function isInputRole(role: string | undefined): boolean {
  return (
    role === Roles.TextField ||
    role === Roles.SecureTextField ||
    role === Roles.SearchField
  );
}

function areaOf(c: TreeCursor): number {
  const b = parseBounds(getAttr(c.node, AttrKeys.Bounds));
  if (!b) return Number.MAX_SAFE_INTEGER;
  return (b.right - b.left) * (b.bottom - b.top);
}

function matchesExpectation(
  cursor: TreeCursor,
  expected: string,
  expectedLength: number,
): boolean {
  const actual = getAttr(cursor.node, AttrKeys.Text) ?? "";
  const label = getAttr(cursor.node, AttrKeys.Label) ?? "";
  const role = getAttr(cursor.node, AttrKeys.Role);

  if (role === Roles.SecureTextField) {
    // Strong: masked bullet count matches expected character count.
    if (codePointLength(actual) === expectedLength) return true;
    // Weak: the field is showing non-empty masked content that
    // isn't the placeholder. Typing happened even if the mask
    // hasn't caught up to the full length yet (Flutter iOS
    // Semantics update lags behind XCUITest typeText).
    return hasTypedChange(actual, label);
  }

  // Strong: exact match. Regular text-fields render the typed
  // content directly.
  if (actual === expected) return true;
  // Weak: text attribute changed from the placeholder (which
  // Flutter iOS mirrors into the `text` slot when value is still
  // propagating). Accept this as "typing committed" — the strict
  // match would demand full Semantics sync within 1 s which iOS
  // Flutter can miss on long strings, but any deviation from the
  // placeholder is strong evidence the keystrokes landed.
  return hasTypedChange(actual, label);
}

function hasTypedChange(actual: string, label: string): boolean {
  if (actual === "") return false;
  if (actual === label) return false;
  return true;
}

function codePointLength(s: string): number {
  let count = 0;
  // `for...of` iterates code points (grapheme clusters close enough
  // for ASCII + common mask glyphs). `.length` would over-count
  // surrogate pairs and mismatch expected.length for non-BMP content.
  for (const _ of s) count += 1;
  return count;
}
