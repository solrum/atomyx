import type {
  ScenarioDefinition,
  NetworkCapture,
  ScriptArtifacts,
} from "@atomyx/shared/script";
import type { Orchestra } from "@atomyx/driver/orchestra";
import type { AnyCommandDefinition } from "@atomyx/driver/script";
import type { Clock, Logger } from "@atomyx/core/infra";
import { ScriptRunner } from "./script-runner.js";
import type { ScriptResult, StepEvent } from "./script-runner.js";
import { parseScript } from "../parser/yaml-parser.js";

export type ScenarioEvent =
  | {
      readonly type: "scenarioStarted";
      readonly scenarioName: string;
      readonly totalScripts: number;
    }
  | {
      readonly type: "scriptStarted";
      readonly scriptIndex: number;
      readonly scriptPath: string;
    }
  | {
      readonly type: "scriptCompleted";
      readonly scriptIndex: number;
      readonly scriptPath: string;
      readonly status: ScriptRunStatus;
      readonly durationMs: number;
      readonly failedAtStep?: number;
    }
  | {
      readonly type: "scenarioCompleted";
      readonly ok: boolean;
      readonly totalScripts: number;
      readonly passedScripts: number;
      readonly durationMs: number;
    };

export type ScriptRunStatus = "passed" | "failed" | "skipped" | "errored";

export interface ScenarioScriptResult {
  readonly scriptIndex: number;
  readonly scriptPath: string;
  readonly status: ScriptRunStatus;
  readonly result?: ScriptResult;
  readonly error?: string;
  readonly durationMs: number;
}

export interface ScenarioResult {
  readonly scenarioName: string;
  readonly ok: boolean;
  readonly scripts: readonly ScenarioScriptResult[];
  readonly totalScripts: number;
  readonly passedScripts: number;
  readonly durationMs: number;
}

export interface ScenarioRunnerDeps {
  readonly orchestra: Orchestra;
  readonly clock: Clock;
  readonly logger: Logger;
  readonly networkCapture?: NetworkCapture;
  readonly commands?: readonly AnyCommandDefinition[];
  /**
   * Load a child script's YAML text by path. Paths arrive exactly
   * as authored in the scenario `scripts` list; the loader is
   * responsible for resolving them (typically against the scenario
   * file's directory) and reading from disk or another source.
   */
  readonly loadScript: (path: string) => string | Promise<string>;
  /**
   * Forwarded to every child `ScriptRunner` so the host can stream
   * step events alongside scenario-level events. Each event also
   * receives a `scriptIndex` field bookended by `scriptStarted` /
   * `scriptCompleted` from `onScenarioEvent`, so consumers can
   * attribute step events to their owning script.
   */
  readonly onStep?: (event: StepEvent) => void;
  /** Hook for scenario-level lifecycle events. */
  readonly onScenarioEvent?: (event: ScenarioEvent) => void;
  /**
   * Cooperative cancellation. Checked before each child script
   * starts. Mid-script cancellation propagates via the same
   * signal threaded into each `ScriptRunner`.
   */
  readonly signal?: AbortSignal;
}

/**
 * Runs an ordered group of independent scripts as one logical
 * unit. Each script gets its own fresh `ScriptRunner` so variables,
 * captures, and artifacts do not leak across scripts. The scenario
 * fails if any individual script fails — `onFailure: continue`
 * still surfaces an aggregate `ok: false` but lets every script
 * run so a regression sweep reports all failures together.
 *
 * Env merge: scenario `env` is the default, the child script's own
 * `env` overrides per-key. Child scripts therefore stay runnable
 * standalone with the same variable names.
 */
export class ScenarioRunner {
  constructor(private readonly deps: ScenarioRunnerDeps) {}

  private emit(event: ScenarioEvent): void {
    const hook = this.deps.onScenarioEvent;
    if (!hook) return;
    try {
      hook(event);
    } catch {
      /* consumer misbehaviour must not crash the run */
    }
  }

  async run(scenario: ScenarioDefinition): Promise<ScenarioResult> {
    const startedAt = this.deps.clock.now();
    const onFailure = scenario.onFailure ?? "stop";
    const scenarioEnv = scenario.env ?? {};
    const total = scenario.scripts.length;

    this.emit({
      type: "scenarioStarted",
      scenarioName: scenario.name,
      totalScripts: total,
    });

    const results: ScenarioScriptResult[] = [];
    let stopRequested = false;

    for (let i = 0; i < total; i++) {
      this.deps.signal?.throwIfAborted();
      const scriptPath = scenario.scripts[i]!;

      if (stopRequested) {
        results.push({
          scriptIndex: i,
          scriptPath,
          status: "skipped",
          durationMs: 0,
        });
        continue;
      }

      this.emit({
        type: "scriptStarted",
        scriptIndex: i,
        scriptPath,
      });
      const scriptStart = this.deps.clock.now();
      let status: ScriptRunStatus;
      let result: ScriptResult | undefined;
      let errorMessage: string | undefined;
      let failedAtStep: number | undefined;

      try {
        const yaml = await this.deps.loadScript(scriptPath);
        const script = parseScript(yaml, scenarioEnv);
        const runner = new ScriptRunner({
          orchestra: this.deps.orchestra,
          clock: this.deps.clock,
          logger: this.deps.logger,
          networkCapture: this.deps.networkCapture,
          commands: this.deps.commands,
          signal: this.deps.signal,
          onStep: this.deps.onStep,
        });
        result = await runner.run(script);
        status = result.ok ? "passed" : "failed";
        failedAtStep = result.failedAtStep;
      } catch (err) {
        status = "errored";
        errorMessage = err instanceof Error ? err.message : String(err);
      }

      const durationMs = this.deps.clock.now() - scriptStart;
      results.push({
        scriptIndex: i,
        scriptPath,
        status,
        result,
        error: errorMessage,
        durationMs,
      });
      this.emit({
        type: "scriptCompleted",
        scriptIndex: i,
        scriptPath,
        status,
        durationMs,
        failedAtStep,
      });

      if (status !== "passed" && onFailure === "stop") {
        stopRequested = true;
      }
    }

    const passedScripts = results.filter((s) => s.status === "passed").length;
    const ok = results.every((s) => s.status === "passed");
    const durationMs = this.deps.clock.now() - startedAt;

    this.emit({
      type: "scenarioCompleted",
      ok,
      totalScripts: total,
      passedScripts,
      durationMs,
    });

    return {
      scenarioName: scenario.name,
      ok,
      scripts: results,
      totalScripts: total,
      passedScripts,
      durationMs,
    };
  }
}

/**
 * Helper for tests / hosts that already have script YAML strings
 * keyed by path. Returns a `loadScript` callback that throws when
 * an unknown path is requested.
 */
export function memoryScriptLoader(
  scripts: Readonly<Record<string, string>>,
): (path: string) => string {
  return (path: string) => {
    const yaml = scripts[path];
    if (yaml === undefined) {
      throw new Error(`scenario referenced unknown script: ${path}`);
    }
    return yaml;
  };
}

/* type-only re-export so callers don't need a second import */
export type { ScriptArtifacts };
