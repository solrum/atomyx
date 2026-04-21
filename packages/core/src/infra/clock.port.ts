/**
 * Time source abstraction. Every polling loop, timeout, retry, and
 * wait in core MUST go through a `Clock` — direct `Date.now()` /
 * `setTimeout()` calls are forbidden in core modules.
 *
 * Rationale: lets tests inject a `FakeClock` to fast-forward time
 * deterministically. Scroll-search with a 2-minute budget runs in
 * microseconds under test; retry-with-backoff is provable instead
 * of sleep-padded.
 */
export interface Clock {
  /** Current wall-clock time in milliseconds since epoch. */
  now(): number;
  /** Resolve after at least `ms` milliseconds have elapsed. */
  sleep(ms: number): Promise<void>;
}

/** Production clock backed by the host runtime. */
export class SystemClock implements Clock {
  now(): number {
    return Date.now();
  }
  sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }
}

/**
 * Test clock. Advances only when `advance()` is called explicitly.
 * `sleep()` returns a promise that resolves the next time the clock
 * is advanced past the target time.
 *
 * Semantics:
 *   - `sleep(100)` at t=0, then `advance(50)` → still pending.
 *   - `advance(60)` → resolved (total 110 ≥ 100).
 *   - Multiple pending sleeps resolve in deadline order on each
 *     `advance` call.
 */
export class FakeClock implements Clock {
  private current: number;
  private pending: Array<{ deadline: number; resolve: () => void }> = [];

  constructor(startAtMs = 0) {
    this.current = startAtMs;
  }

  now(): number {
    return this.current;
  }

  sleep(ms: number): Promise<void> {
    if (ms <= 0) return Promise.resolve();
    return new Promise((resolve) => {
      this.pending.push({ deadline: this.current + ms, resolve });
    });
  }

  /** Fast-forward time by `ms`, resolving any expired sleeps. */
  advance(ms: number): void {
    this.current += ms;
    const stillPending: typeof this.pending = [];
    const toResolve: Array<() => void> = [];
    for (const p of this.pending) {
      if (p.deadline <= this.current) {
        toResolve.push(p.resolve);
      } else {
        stillPending.push(p);
      }
    }
    this.pending = stillPending;
    for (const r of toResolve) r();
  }

  /** Diagnostic — how many sleeps are still waiting. */
  pendingCount(): number {
    return this.pending.length;
  }
}
