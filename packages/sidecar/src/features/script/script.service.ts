import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve as resolvePath } from "node:path";
import {
  parseScript,
  parseScenario,
  ScriptRunner,
  ScenarioRunner,
} from "@atomyx/script";
import type { ScriptResult, ScenarioResult } from "@atomyx/script";
import { SystemClock, NoopLogger } from "@atomyx/driver";
import type { EventBus } from "../../infra/events/event-bus.js";
import type { Session } from "../../infra/session/session.js";

export interface ScriptRunnerServiceDeps {
  readonly session: Session;
  readonly events: EventBus;
}

/**
 * Executes script and scenario YAML against the current session's
 * device and emits lifecycle events on the EventBus so the host UI
 * can render progress live.
 *
 * Single-script events: runStarted / stepStarted / stepCompleted /
 * runCompleted / runErrored.
 *
 * Scenario events: scenarioStarted / scriptStarted / scriptCompleted
 * / scenarioCompleted, plus the per-step events of each child run.
 */
export class ScriptRunnerService {
  private readonly session: Session;
  private readonly events: EventBus;
  private currentRun: { readonly runId: string; readonly abort: AbortController } | null = null;

  constructor(deps: ScriptRunnerServiceDeps) {
    this.session = deps.session;
    this.events = deps.events;
  }

  async run(yaml: string): Promise<{ readonly runId: string; readonly result: ScriptResult }> {
    const device = this.session.requireDevice();
    if (this.currentRun) {
      throw new Error("A run is already in progress");
    }
    const runId = randomUUID();
    const abort = new AbortController();
    this.currentRun = { runId, abort };
    this.session.setActiveRunId(runId);

    const script = parseScript(yaml);
    const events = this.events;
    const runner = new ScriptRunner({
      orchestra: device.orchestra,
      clock: new SystemClock(),
      logger: new NoopLogger(),
      signal: abort.signal,
      onStep: (e) => {
        events.emit({
          event: e.type,
          payload: { runId, ...e },
        });
      },
    });

    this.events.emit({
      event: "runStarted",
      payload: { runId, startedAt: Date.now(), scriptName: script.name },
    });

    try {
      const result = await runner.run(script);
      this.events.emit({
        event: "runCompleted",
        payload: {
          runId,
          ok: result.ok,
          passed: result.passedSteps,
          total: result.totalSteps,
          durationMs: result.durationMs,
        },
      });
      return { runId, result };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.events.emit({
        event: "runErrored",
        payload: { runId, message },
      });
      throw err;
    } finally {
      this.currentRun = null;
      this.session.setActiveRunId(null);
    }
  }

  async runScenario(
    yaml: string,
    cwd: string,
  ): Promise<{ readonly runId: string; readonly result: ScenarioResult }> {
    const device = this.session.requireDevice();
    if (this.currentRun) {
      throw new Error("A run is already in progress");
    }
    const runId = randomUUID();
    const abort = new AbortController();
    this.currentRun = { runId, abort };
    this.session.setActiveRunId(runId);

    const scenario = parseScenario(yaml);
    const events = this.events;
    const runner = new ScenarioRunner({
      orchestra: device.orchestra,
      clock: new SystemClock(),
      logger: new NoopLogger(),
      signal: abort.signal,
      loadScript: (relPath) =>
        readFileSync(resolvePath(cwd, relPath), "utf-8"),
      onStep: (e) => {
        events.emit({
          event: e.type,
          payload: { runId, ...e },
        });
      },
      onScenarioEvent: (e) => {
        events.emit({
          event: e.type,
          payload: { runId, ...e },
        });
      },
    });

    this.events.emit({
      event: "runStarted",
      payload: {
        runId,
        startedAt: Date.now(),
        scenarioName: scenario.name,
        totalScripts: scenario.scripts.length,
      },
    });

    try {
      const result = await runner.run(scenario);
      this.events.emit({
        event: "runCompleted",
        payload: {
          runId,
          ok: result.ok,
          passedScripts: result.passedScripts,
          totalScripts: result.totalScripts,
          durationMs: result.durationMs,
        },
      });
      return { runId, result };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.events.emit({
        event: "runErrored",
        payload: { runId, message },
      });
      throw err;
    } finally {
      this.currentRun = null;
      this.session.setActiveRunId(null);
    }
  }

  stop(): void {
    if (!this.currentRun) return;
    this.currentRun.abort.abort();
    this.events.emit({
      event: "runStopRequested",
      payload: { runId: this.currentRun.runId },
    });
  }

  isRunning(): boolean {
    return this.currentRun !== null;
  }
}
