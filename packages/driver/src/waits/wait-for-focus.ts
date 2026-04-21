import type { Clock } from "@atomyx/core/infra";
import type { Driver } from "../driver/driver.port.js";
import { compileSelector } from "../selectors/priority-broadening.js";
import type { Selector } from "../selectors/selector.js";
import { findFocusedNode } from "../state/focus-state.js";
import { parseBounds, boundsIntersect, type Bounds } from "../tree/bounds.js";
import { AttrKeys, getAttr } from "../tree/tree-node.js";
import type { TreeCursor } from "../tree/tree-cursor.js";
import { fromTree } from "../filters/element-filter.js";
import { waitUntil } from "./wait-until.js";

/**
 * Wait until the element matching `selector` has INPUT FOCUS.
 *
 * Flutter/RN focus shifts lag coordinate taps by ~200-500ms. The
 * previous band-aid was `sleep(300)` after tapping a text field;
 * that under-waits on slow devices and over-waits on snappy ones.
 * This primitive polls `hierarchy()` instead, returning as soon as
 * the tree reports focus on the right element.
 *
 * Match semantics — bounds-contains instead of node identity:
 *
 *   Flutter merges semantics. The "focused" node reported by the
 *   a11y tree may be a descendant (the underlying UITextInput)
 *   whose bounds fully coincide with the parent's bounds (the
 *   Semantics wrapper we matched against). Node identity would
 *   miss this case even though it's the same logical element.
 *
 *   Rule: a candidate from `selector` is considered focused when
 *   ITS bounds intersect the focused node's bounds. Intersection
 *   (not equality) tolerates minor rounding between the Semantics
 *   wrapper and the native input.
 *
 * Returns the matching cursor FROM THE SELECTOR'S CANDIDATE LIST —
 * callers typically need the selector-resolved node for downstream
 * actions like bounds → tap coordinate, not the raw focused node.
 */
export interface WaitForFocusOptions {
  readonly driver: Driver;
  readonly selector: Selector;
  readonly clock: Clock;
  readonly timeoutMs?: number;
  readonly intervalMs?: number;
}

export async function waitForFocus(
  opts: WaitForFocusOptions,
): Promise<TreeCursor> {
  const filter = compileSelector(opts.selector);
  const result = await waitUntil<TreeCursor | null>({
    fetch: async () => {
      const tree = await opts.driver.hierarchy();
      const focused = findFocusedNode(tree);
      if (!focused) return null;
      const focusedBounds = parseBounds(getAttr(focused.node, AttrKeys.Bounds));
      if (!focusedBounds) return null;
      const candidates = filter(fromTree(tree));
      for (const c of candidates) {
        const cb = parseBounds(getAttr(c.node, AttrKeys.Bounds));
        if (!cb) continue;
        if (sameBounds(cb, focusedBounds) || boundsIntersect(cb, focusedBounds)) {
          return c;
        }
      }
      return null;
    },
    predicate: (v): v is TreeCursor => v !== null,
    timeoutMs: opts.timeoutMs ?? 1500,
    intervalMs: opts.intervalMs ?? 50,
    clock: opts.clock,
    kind: "waitForFocus",
  });
  // Predicate narrowed non-null, but the generic signature still
  // carries `TreeCursor | null` through — unwrap explicitly.
  if (!result) throw new Error("waitForFocus: unreachable — predicate narrowed");
  return result;
}

function sameBounds(a: Bounds, b: Bounds): boolean {
  return (
    a.left === b.left &&
    a.top === b.top &&
    a.right === b.right &&
    a.bottom === b.bottom
  );
}
