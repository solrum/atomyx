import {
  type ElementFilter,
  idMatches,
  textMatches,
  labelMatches,
  hintMatches,
  valueMatches,
  roleIs,
  isEnabled,
  isClickable,
  isFocused,
  intersect,
  nth as nthFilter,
} from "../filters/element-filter.js";
import type { TreeCursor } from "../tree/tree-cursor.js";
import type { Selector } from "./selector.js";

/**
 * Priority broadening — the Atomyx agent-ergonomic policy that
 * sits on top of the pure filter-composition primitives.
 *
 * Problem statement: an AI agent passes `{text: "Login"}` as a
 * selector. The actual element may have `id="login_btn"` AND
 * `text="Login"` AND `label="Sign in button"`. The agent doesn't
 * know which field will match — expecting it to guess is hostile.
 * Priority broadening says: "try each content field the caller
 * provided, in a prioritized order, and return the first one that
 * resolves to a non-empty match".
 *
 * Priority order (most specific → most fuzzy):
 *
 *   1. `id`     — stable cross-release identifier
 *   2. `label`  — accessibility label (usually matches visible text
 *                 with deliberate a11y phrasing)
 *   3. `text`   — visible text content
 *   4. `value`  — current value (for inputs / sliders)
 *   5. `hint`   — placeholder (only useful when nothing else works)
 *
 * Non-content constraints (`role`, `enabled`, `clickable`,
 * `focused`) are NOT part of the priority chain — they are
 * AND-ed into every candidate filter as additional constraints.
 * "I want a button with text Login" means `button AND text=Login`,
 * not "try button, fall back to text".
 *
 * Finally, `nth` applies AFTER broadening to pick one match from
 * the winning filter's result list.
 *
 * Caller code composes this with `Finder.find(filter, ...)`:
 *
 *     const filter = compileSelector({ text: "Login", role: "button" });
 *     const result = await finder.findOne(filter);
 *
 * Advanced callers who need AND/OR between content fields should
 * SKIP `compileSelector` and build the filter directly with
 * `intersect` / `union` from the filter module. Priority
 * broadening is specifically the "agent passed me a simple
 * selector, give me a sensible default resolution" case.
 */
export function compileSelector(s: Selector): ElementFilter {
  // Build the constraint list (AND-ed with every content filter).
  const constraints: ElementFilter[] = [];
  if (s.role !== undefined) constraints.push(roleIs(s.role));
  if (s.enabled === true) constraints.push(isEnabled());
  if (s.clickable === true) constraints.push(isClickable());
  if (s.focused === true) constraints.push(isFocused());

  // Build the priority-ordered content filter list. Only the
  // fields the caller provided show up here — priority is
  // implicit in array order. When the caller provided `text` but
  // not `label`, also try `label` as a fallback: iOS only sets
  // `attributes.text` on genuine staticText leaves, so a cross-
  // platform `{text: "Sign in"}` query against a button needs to
  // fall back to the a11y `label` to match.
  const priorityFilters: ElementFilter[] = [];
  if (s.id !== undefined) priorityFilters.push(idMatches(s.id));
  if (s.label !== undefined) priorityFilters.push(labelMatches(s.label));
  if (s.text !== undefined) {
    priorityFilters.push(textMatches(s.text));
    if (s.label === undefined) {
      priorityFilters.push(labelMatches(s.text));
    }
  }
  if (s.value !== undefined) priorityFilters.push(valueMatches(s.value));
  if (s.hint !== undefined) priorityFilters.push(hintMatches(s.hint));

  // If no content field was provided, the filter is constraint-
  // only. This is legal but will match many elements — callers
  // should add `nth` or use direct filter composition.
  if (priorityFilters.length === 0) {
    const constraintOnly: ElementFilter =
      constraints.length > 0
        ? intersect(...constraints)
        : (cursors) => [...cursors];
    return s.nth !== undefined ? nthFilter(constraintOnly, s.nth) : constraintOnly;
  }

  const broadened: ElementFilter = (cursors) => {
    for (const content of priorityFilters) {
      const combined: ElementFilter =
        constraints.length > 0
          ? intersect(content, ...constraints)
          : content;
      const result = combined(cursors);
      if (result.length > 0) {
        // `nth` is positional — skip ranking to preserve tree
        // order, which is what the caller meant by "the Nth match".
        if (s.nth !== undefined) return result;
        return rankCandidates(result);
      }
    }
    return [];
  };

  return s.nth !== undefined ? nthFilter(broadened, s.nth) : broadened;
}

/**
 * Stable-sort candidates so interactive + focused elements rank
 * above inert duplicates. Fixes a real-world ambiguity: when the
 * agent says `tap("Verify")` on a screen where a non-clickable
 * title AND a clickable button both carry the text "Verify", the
 * button should win even when the title appears first in the
 * document tree.
 *
 * Scoring (highest first):
 *
 *   +1 if clickable === true
 *   +1 if focused === true
 *
 * Stable: equal-scored candidates keep their original document
 * order, preserving deterministic replay when nothing semantically
 * distinguishes them.
 *
 * Opt-out: when the caller specifies `nth`, we skip ranking —
 * `nth` is positional by definition.
 */
function rankCandidates(cursors: TreeCursor[]): TreeCursor[] {
  // Decorate-sort-undecorate with original index so the sort is
  // stable without relying on Array.prototype.sort's engine-
  // specific stability (guaranteed in V8 since ES2019, but spelling
  // it out documents the intent).
  const decorated = cursors.map((c, index) => ({
    c,
    index,
    score: scoreCursor(c),
  }));
  decorated.sort((a, b) => {
    if (a.score !== b.score) return b.score - a.score;
    return a.index - b.index;
  });
  return decorated.map((d) => d.c);
}

function scoreCursor(c: TreeCursor): number {
  let score = 0;
  if (c.node.clickable === true) score += 1;
  if (c.node.focused === true) score += 1;
  return score;
}
