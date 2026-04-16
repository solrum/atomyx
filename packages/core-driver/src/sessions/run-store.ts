/**
 * Run lifecycle tracking for the core-driver module.
 *
 * A "run" is a bounded test session with metadata:
 * - Name (human description)
 * - Source (which mode: exploratory, regression, bug-repro)
 * - Started / finished timestamps
 * - Action counter (increments on every mutating tool call)
 * - Status (running | passed | failed | error)
 * - Findings (list of bugs recorded during the run)
 *
 * The `RunStore` is an in-memory singleton per server instance.
 * Only one run can be active at a time; starting a new run while
 * one is active rolls the old one over as `status: "error"` with
 * an explanatory reason (the agent forgot to call `finish_run`).
 *
 * Run data is deliberately NOT persisted here — persistence is
 * a separate concern handled by the `Reporter` port (planned).
 * The store just holds live state.
 */

export interface RunBugReport {
  readonly id: string;
  readonly title: string;
  readonly description: string;
  readonly screenshotPath?: string;
  readonly timestamp: number;
}

export interface RunRecord {
  readonly id: string;
  readonly name: string;
  readonly source: string;
  readonly startedAt: number;
  finishedAt?: number;
  status: "running" | "passed" | "failed" | "error";
  actionCount: number;
  readonly findings: RunBugReport[];
}

/**
 * Return shape of `RunStore.start`. `run` is the new active run.
 * `erroredPredecessor` is populated iff a previously-running run was
 * force-errored to make room for this one (agent forgot to call
 * `finish_run`) — the tool layer should persist this record so the
 * lost run's trail isn't dropped silently. The `reason` field on the
 * predecessor explains why it was force-closed.
 */
export interface StartRunResult {
  readonly run: RunRecord;
  readonly erroredPredecessor?: RunRecord;
}

export class RunStore {
  private active: RunRecord | null = null;
  private nextId = 1;

  /**
   * Start a new run. If one is already active, mark the old one as
   * errored (implicit finish) and return it alongside the new run so
   * the caller can persist the force-closed record. Callers that
   * ignore `erroredPredecessor` silently drop the old run's history.
   */
  start(opts: { name: string; source?: string }): StartRunResult {
    let erroredPredecessor: RunRecord | undefined;
    if (this.active && this.active.status === "running") {
      this.active.status = "error";
      this.active.finishedAt = Date.now();
      erroredPredecessor = this.active;
    }
    const run: RunRecord = {
      id: `run-${Date.now()}-${this.nextId++}`,
      name: opts.name,
      source: opts.source ?? "unknown",
      startedAt: Date.now(),
      status: "running",
      actionCount: 0,
      findings: [],
    };
    this.active = run;
    return { run, erroredPredecessor };
  }

  /**
   * Finish the currently-active run with a verdict. No-op if
   * there is no active run.
   */
  finish(status: "passed" | "failed" | "error" = "passed"): RunRecord | null {
    if (!this.active) return null;
    this.active.status = status;
    this.active.finishedAt = Date.now();
    const finished = this.active;
    this.active = null;
    return finished;
  }

  /** Return the currently-active run or null. */
  current(): RunRecord | null {
    return this.active;
  }

  /**
   * Increment the action counter on the active run. Called by
   * Orchestra wrappers or tool dispatch plumbing that wants to
   * track "actions per run". Safe to call when no run is active
   * (it becomes a no-op).
   */
  incrementActions(): void {
    if (this.active) this.active.actionCount++;
  }

  /**
   * Record a bug finding against the active run. Returns the
   * finding id so the tool can reference it in logs. Throws if
   * no run is active — a bug without a run context is a
   * programming error at the tool layer.
   */
  recordBug(opts: {
    title: string;
    description: string;
    screenshotPath?: string;
  }): RunBugReport {
    if (!this.active) {
      throw new Error(
        "recordBug: no active run. Call start_run before report_bug.",
      );
    }
    // Bug id is derived from the run id so every bug is
    // deterministically namespaced to its run — agents can see
    // which run a bug belongs to from the id alone, and the
    // `bugs/<bugId>/screenshot` storage key is a stable sibling of
    // `bugs/<bugId>` without depending on `Date.now()` alignment
    // (two bugs reported in the same millisecond used to collide
    // under the old timestamp-based scheme).
    const bug: RunBugReport = {
      id: `${this.active.id}-bug-${this.active.findings.length + 1}`,
      title: opts.title,
      description: opts.description,
      screenshotPath: opts.screenshotPath,
      timestamp: Date.now(),
    };
    this.active.findings.push(bug);
    return bug;
  }
}
