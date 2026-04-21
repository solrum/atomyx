import type { Clock } from "@atomyx/core/infra";
import type { Driver } from "../driver/driver.port.js";
import { readKeyboardState, type KeyboardState } from "../state/focus-state.js";
import { waitUntil } from "./wait-until.js";

/**
 * Wait until the on-screen keyboard is either visible or hidden,
 * as declared by `expectVisible`. Polls `hierarchy()` and reads
 * keyboard state host-side via `readKeyboardState`.
 *
 * Used after `driver.hideKeyboard()` to confirm the dismissal
 * animation completed before interacting with elements below the
 * former keyboard frame. The dismiss gesture dispatch returns
 * immediately; the tree takes ~200-400ms to reflect the new state
 * depending on the platform.
 *
 * Also usable the other way: after a tap on a text field, wait for
 * the keyboard to APPEAR before dispatching follow-up actions that
 * depend on keyboard presence (e.g. reading `getKeyboardState`
 * for key geometry).
 */
export interface WaitForKeyboardOptions {
  readonly driver: Driver;
  readonly expectVisible: boolean;
  readonly clock: Clock;
  readonly timeoutMs?: number;
  readonly intervalMs?: number;
}

export async function waitForKeyboard(
  opts: WaitForKeyboardOptions,
): Promise<KeyboardState> {
  return waitUntil<KeyboardState>({
    fetch: async () => readKeyboardState(await opts.driver.hierarchy()),
    predicate: (state) => state.visible === opts.expectVisible,
    timeoutMs: opts.timeoutMs ?? 1000,
    intervalMs: opts.intervalMs ?? 50,
    clock: opts.clock,
    kind: `waitForKeyboard(visible=${opts.expectVisible})`,
  });
}
