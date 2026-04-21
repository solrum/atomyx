import type { Clock } from "@atomyx/core/infra";
import type { Driver } from "../driver/driver.port.js";
import { compileSelector } from "../selectors/priority-broadening.js";
import type { Selector } from "../selectors/selector.js";
import { findFocusedNode, readKeyboardState } from "../state/focus-state.js";
import { parseBounds, boundsIntersect } from "../tree/bounds.js";
import { AttrKeys, getAttr } from "../tree/tree-node.js";
import { fromTree } from "../filters/element-filter.js";
import { waitUntil } from "./wait-until.js";

/**
 * Wait until the input field at `selector` is ready to receive text.
 * Broader than `waitForFocus`: handles platforms where Flutter / RN
 * custom text inputs don't expose `hasKeyboardFocus` through the
 * a11y tree at all (iOS Flutter TextField wraps a UITextField whose
 * focus state isn't mirrored onto the Semantics node consumed by
 * `XCUIElementSnapshot`).
 *
 * Multi-signal predicate (any one satisfies):
 *
 *   A. A node reports `focused=true` AND its bounds intersect a
 *      selector-matched candidate's bounds. Strict signal, matches
 *      native input fields on both platforms.
 *
 *   B. The on-screen keyboard is visible AND no OTHER node reports
 *      focused=true. Fallback for Flutter/RN custom inputs: we just
 *      tapped the target; the keyboard appearing is material
 *      evidence that focus landed somewhere sensible, and "no
 *      conflicting focus" rules out the case where a prior input
 *      field is still claiming focus.
 *
 * Why this is specific to "input ready" not "any focus": only text
 * inputs need the keyboard-fallback signal. A non-input element
 * (button, cell, switch) never opens the keyboard, so the fallback
 * would be meaningless. Callers that want strict focus on
 * non-inputs use `waitForFocus` directly.
 */
export interface WaitForInputReadyOptions {
  readonly driver: Driver;
  readonly selector: Selector;
  readonly clock: Clock;
  readonly timeoutMs?: number;
  readonly intervalMs?: number;
}

export async function waitForInputReady(
  opts: WaitForInputReadyOptions,
): Promise<void> {
  const filter = compileSelector(opts.selector);
  await waitUntil<boolean>({
    fetch: async () => {
      const tree = await opts.driver.hierarchy();
      const candidates = filter(fromTree(tree));
      const focused = findFocusedNode(tree);
      const keyboard = readKeyboardState(tree);

      // Signal A — strict focus on the selector's target.
      if (focused) {
        const focusedBounds = parseBounds(
          getAttr(focused.node, AttrKeys.Bounds),
        );
        if (focusedBounds) {
          for (const c of candidates) {
            const cb = parseBounds(getAttr(c.node, AttrKeys.Bounds));
            if (cb && boundsIntersect(cb, focusedBounds)) return true;
          }
        }
        // Focused node reported but not our target — probably stale
        // focus from a previous field. Keep waiting rather than
        // falling through to the keyboard-fallback (that would
        // accept a wrong-target focus as "ready").
        return false;
      }

      // Signal B — keyboard fallback for platforms that don't
      // surface focus on custom text inputs.
      if (keyboard.visible) return true;
      return false;
    },
    predicate: (v) => v === true,
    timeoutMs: opts.timeoutMs ?? 1500,
    intervalMs: opts.intervalMs ?? 50,
    clock: opts.clock,
    kind: "waitForInputReady",
  });
}
