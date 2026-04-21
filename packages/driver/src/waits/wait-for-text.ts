import type { Clock } from "@atomyx/core/infra";
import type { Driver } from "../driver/driver.port.js";
import { compileSelector } from "../selectors/priority-broadening.js";
import type { Selector } from "../selectors/selector.js";
import { AttrKeys, getAttr } from "../tree/tree-node.js";
import type { TreeCursor } from "../tree/tree-cursor.js";
import { fromTree } from "../filters/element-filter.js";
import { waitUntil } from "./wait-until.js";

/**
 * Wait until the element matching `selector` reports `expected` as
 * its `text` attribute (or `value`, whichever is present — the
 * normalizer mirrors value→text so consumers only need to check
 * `text`).
 *
 * Used by `Orchestra.inputText` to verify that a text input
 * actually accepted the typed characters. ACTION_SET_TEXT on
 * Android against Flutter obscureText fields silently no-ops — the
 * agent believes the type succeeded, but the tree shows empty.
 * This primitive catches that within its timeout and lets the
 * caller retry via a different strategy (per-key tapping).
 *
 * `expected` can be a string (exact match) or a RegExp (pattern).
 * Regex is useful for partial matches, whitespace-normalized text,
 * or prefix-only verification during long typing animations.
 */
export interface WaitForTextOptions {
  readonly driver: Driver;
  readonly selector: Selector;
  readonly expected: string | RegExp;
  readonly clock: Clock;
  readonly timeoutMs?: number;
  readonly intervalMs?: number;
}

export async function waitForText(opts: WaitForTextOptions): Promise<TreeCursor> {
  const filter = compileSelector(opts.selector);
  const result = await waitUntil<TreeCursor | null>({
    fetch: async () => {
      const tree = await opts.driver.hierarchy();
      const candidates = filter(fromTree(tree));
      for (const c of candidates) {
        const text = getAttr(c.node, AttrKeys.Text) ?? "";
        if (matches(text, opts.expected)) return c;
      }
      return null;
    },
    predicate: (v): v is TreeCursor => v !== null,
    timeoutMs: opts.timeoutMs ?? 1000,
    intervalMs: opts.intervalMs ?? 50,
    clock: opts.clock,
    kind: "waitForText",
  });
  if (!result) throw new Error("waitForText: unreachable — predicate narrowed");
  return result;
}

function matches(actual: string, expected: string | RegExp): boolean {
  return typeof expected === "string" ? actual === expected : expected.test(actual);
}
