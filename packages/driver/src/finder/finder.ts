import type { Driver } from "../driver/driver.port.js";
import type { Clock } from "@atomyx/core/infra";
import type { Logger } from "@atomyx/core/infra";
import { NoopLogger } from "@atomyx/core/infra";
import type { ElementFilter } from "../filters/element-filter.js";
import type { TreeCursor } from "../tree/tree-cursor.js";
import { fromTree } from "../filters/element-filter.js";

/**
 * Find elements in the driver's current UI hierarchy. Exposes a
 * polling-with-timeout wait semantics suitable for "element might
 * appear shortly" flows (post-tap transition, loading state, etc).
 *
 * `Finder` is the bridge between pure filter composition and a
 * live driver. Takes a `Driver.hierarchy()` snapshot, runs the
 * filter, returns cursors. The wait variant polls on a clock-
 * driven interval until the filter yields a non-empty result or
 * the budget expires.
 *
 * Dependency injection:
 *
 *   - `driver`: narrowed to just `hierarchy()` — finder does NOT
 *     tap, scroll, or mutate device state. Downstream components
 *     (ScrollController, Orchestra) compose Finder with other
 *     driver methods.
 *
 *   - `clock`: every timeout / sleep goes through the clock so
 *     tests can use `FakeClock` to fast-forward.
 *
 *   - `logger`: optional structured logging for diagnostics.
 *     Defaults to `NoopLogger`.
 *
 * Contract:
 *
 *   - `find` returns the complete match list synchronously (1
 *     hierarchy call).
 *
 *   - `findOne` returns the first match or `null`. Does NOT
 *     throw on empty — callers decide whether empty is an error.
 *
 *   - `waitFor` polls until non-empty or timeout. Returns the
 *     complete match list on success; throws on timeout with a
 *     structured error containing the final snapshot count.
 */
export interface FinderDeps {
  readonly driver: Pick<Driver, "hierarchy">;
  readonly clock: Clock;
  readonly logger?: Logger;
}

export interface WaitOptions {
  /** Maximum time to wait for the filter to produce matches. */
  readonly timeoutMs: number;
  /**
   * Interval between successive hierarchy polls. Smaller =
   * faster convergence but more RPC load. Default 250ms.
   */
  readonly pollIntervalMs?: number;
  /**
   * External abort signal. When aborted, the in-flight hierarchy
   * RPC is cancelled and the next loop iteration throws the
   * signal's reason (typically an `AbortError`). The MCP tool
   * wrapper aborts this when its own deadline fires so a hung
   * hierarchy call doesn't block the response.
   */
  readonly signal?: AbortSignal;
}

export class FindTimeoutError extends Error {
  constructor(
    public readonly elapsedMs: number,
    public readonly pollCount: number,
  ) {
    super(
      `filter produced no matches after ${elapsedMs}ms ` +
        `(polled ${pollCount} times)`,
    );
    this.name = "FindTimeoutError";
  }
}

export class Finder {
  private readonly logger: Logger;

  constructor(private readonly deps: FinderDeps) {
    this.logger = deps.logger ?? new NoopLogger();
  }

  /**
   * Single-shot find. Captures the hierarchy once and runs the
   * filter against it. No polling, no retry.
   */
  async find(filter: ElementFilter, opts?: { signal?: AbortSignal }): Promise<TreeCursor[]> {
    const tree = await this.deps.driver.hierarchy({ signal: opts?.signal });
    const cursors = fromTree(tree);
    return filter(cursors);
  }

  /**
   * Single-shot find returning the first match or null.
   */
  async findOne(
    filter: ElementFilter,
    opts?: { signal?: AbortSignal },
  ): Promise<TreeCursor | null> {
    const results = await this.find(filter, opts);
    return results[0] ?? null;
  }

  /**
   * Polling find — repeatedly captures the hierarchy and runs
   * the filter until at least one match appears or the timeout
   * expires. Throws `FindTimeoutError` on timeout.
   *
   * Timing:
   *   - First poll fires immediately (no upfront sleep).
   *   - Subsequent polls wait `pollIntervalMs` between attempts.
   *   - The budget is enforced on a per-poll deadline check,
   *     not by racing a timer against the poll call. This lets
   *     a slow hierarchy call complete honestly rather than
   *     being cancelled mid-flight.
   */
  async waitFor(filter: ElementFilter, opts: WaitOptions): Promise<TreeCursor[]> {
    const pollIntervalMs = opts.pollIntervalMs ?? 250;
    const startedAt = this.deps.clock.now();
    const deadline = startedAt + opts.timeoutMs;
    let polls = 0;
    while (true) {
      if (opts.signal?.aborted) {
        throw opts.signal.reason ?? new DOMException("Aborted", "AbortError");
      }
      polls++;
      const results = await this.find(filter, { signal: opts.signal });
      if (results.length > 0) {
        this.logger.debug("waitFor resolved", {
          polls,
          elapsedMs: this.deps.clock.now() - startedAt,
          matches: results.length,
        });
        return results;
      }
      const now = this.deps.clock.now();
      if (now >= deadline) {
        throw new FindTimeoutError(now - startedAt, polls);
      }
      // Clamp sleep to the remaining budget so we don't
      // overshoot.
      const sleepMs = Math.min(pollIntervalMs, deadline - now);
      await this.deps.clock.sleep(sleepMs);
    }
  }

  /**
   * Polling find that returns the first match or throws on
   * timeout. Convenience variant for callers who only care about
   * one element.
   */
  async waitForOne(filter: ElementFilter, opts: WaitOptions): Promise<TreeCursor> {
    const results = await this.waitFor(filter, opts);
    return results[0]!;
  }
}
