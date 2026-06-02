import type { Clock } from "@atomyx/core/infra";

/**
 * Generic poll-until-predicate primitive. Every observation-driven
 * wait in Atomyx (focus, text, keyboard, tree-stable) is a
 * specialization of this.
 *
 * Poll loop:
 *
 *   t=0: fetch()         → predicate? yes → return
 *                         → no, sleep intervalMs
 *   t=50: fetch()        → ...
 *   ...
 *   t=timeoutMs: throw WaitTimeoutError
 *
 * Rationale for always polling AT LEAST once (even at timeout=0):
 * callers that chain `waitFor(cond, {timeoutMs: 0})` want "check
 * now, don't wait" — this returns if the first fetch satisfies,
 * throws otherwise. Matches how Node's `setImmediate` handles zero
 * delays.
 */
export interface WaitUntilOptions<T> {
  /** Data source — called once per poll iteration. */
  readonly fetch: () => Promise<T>;
  /** Return true when the wait should resolve. */
  readonly predicate: (value: T) => boolean;
  /** Hard deadline from first call. Default 2000ms. */
  readonly timeoutMs?: number;
  /** Delay between polls. Default 50ms. */
  readonly intervalMs?: number;
  /** Time source. Required — callers inject `SystemClock` or `FakeClock`. */
  readonly clock: Clock;
  /** Short label used in timeout error messages. */
  readonly kind?: string;
  /**
   * External abort signal. When aborted between polls, the wait
   * throws the signal's reason instead of looping further.
   */
  readonly signal?: AbortSignal;
}

export class WaitTimeoutError extends Error {
  constructor(
    public readonly kind: string,
    public readonly timeoutMs: number,
    public readonly lastValue: unknown,
  ) {
    super(`wait timed out after ${timeoutMs}ms: ${kind}`);
  }
}

export async function waitUntil<T>(opts: WaitUntilOptions<T>): Promise<T> {
  const timeoutMs = opts.timeoutMs ?? 2000;
  const intervalMs = opts.intervalMs ?? 50;
  const kind = opts.kind ?? "waitUntil";
  const deadline = opts.clock.now() + timeoutMs;
  let last: T;
  // First fetch is always performed, even when timeoutMs is 0.
  // The loop re-enters only while the deadline is still ahead.
  // eslint-disable-next-line no-constant-condition
  while (true) {
    if (opts.signal?.aborted) {
      throw opts.signal.reason ?? new DOMException("Aborted", "AbortError");
    }
    last = await opts.fetch();
    if (opts.predicate(last)) return last;
    const remaining = deadline - opts.clock.now();
    if (remaining <= 0) break;
    const nextSleep = Math.min(intervalMs, remaining);
    await opts.clock.sleep(nextSleep);
  }
  throw new WaitTimeoutError(kind, timeoutMs, last);
}
