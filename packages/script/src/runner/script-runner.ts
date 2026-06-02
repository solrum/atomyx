import type {
  ScriptDefinition,
  ScriptStep,
  CapturedRequest,
  ScriptArtifacts,
} from "@atomyx/shared/script";
import type {
  CommandContext,
  CommandResult,
  CommandDefinition,
  AnyCommandDefinition,
} from "@atomyx/driver/script";
import type { Orchestra } from "@atomyx/driver/orchestra";
import type { Clock } from "@atomyx/core/infra";
import type { Logger } from "@atomyx/core/infra";
import type { NetworkCapture } from "@atomyx/shared/script";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { DEFAULT_COMMANDS } from "../commands/index.js";
import { summarizeStep, type StepToken } from "./step-summary.js";
import { parseScript } from "../parser/yaml-parser.js";

export type StepEvent =
  | {
      readonly type: "stepStarted";
      readonly stepIndex: number;
      readonly command: string;
      readonly summary: string;
      readonly tokens: readonly StepToken[];
      readonly depth: number;
      readonly line?: number;
    }
  | {
      readonly type: "stepCompleted";
      readonly stepIndex: number;
      readonly command: string;
      readonly summary: string;
      readonly tokens: readonly StepToken[];
      readonly ok: boolean;
      readonly detail?: string;
      readonly durationMs: number;
      readonly depth: number;
      readonly line?: number;
    };

export interface ScriptRunnerDeps {
  readonly orchestra: Orchestra;
  readonly clock: Clock;
  readonly logger: Logger;
  readonly networkCapture?: NetworkCapture;
  /** Override the built-in command registry. */
  readonly commands?: readonly AnyCommandDefinition[];
  /**
   * Progress hook invoked before each step (stepStarted) and after
   * (stepCompleted). Exceptions thrown from the hook are swallowed
   * so a misbehaving consumer cannot crash the run. Use for
   * sidecar event streaming, live UIs, trace logging.
   */
  readonly onStep?: (event: StepEvent) => void;
  /**
   * Cooperative cancellation. Checked between steps; when aborted
   * the runner throws via `signal.throwIfAborted()`. Mid-step
   * cancellation (inside a long-running command) is not supported
   * — abort takes effect when the current step finishes.
   */
  readonly signal?: AbortSignal;
}

export interface StepResult {
  readonly stepIndex: number;
  readonly command: string;
  readonly ok: boolean;
  readonly detail?: string;
  readonly durationMs: number;
}

export interface ScriptResult {
  readonly scriptName: string;
  readonly ok: boolean;
  readonly steps: readonly StepResult[];
  readonly totalSteps: number;
  readonly passedSteps: number;
  readonly failedAtStep?: number;
  readonly durationMs: number;
  readonly artifacts: ScriptArtifacts;
}

/**
 * Deterministic script runner. Executes a `ScriptDefinition`
 * step by step against an `Orchestra` instance. Fails fast
 * on the first step that returns `ok: false`.
 *
 * No LLM, no agent, no branching — pure sequential execution.
 */
export class ScriptRunner {
  private readonly commandRegistry: Map<string, CommandDefinition>;

  constructor(private readonly deps: ScriptRunnerDeps) {
    this.commandRegistry = new Map();
    const commands = deps.commands ?? DEFAULT_COMMANDS;
    for (const cmd of commands) {
      this.commandRegistry.set(cmd.command, cmd);
    }
  }

  private emitStep(event: StepEvent): void {
    const hook = this.deps.onStep;
    if (!hook) return;
    try {
      hook(event);
    } catch {
      /* consumer misbehaviour must not crash the run */
    }
  }

  /** Track completed requires flows for dedup. */
  private completedRequires = new Set<string>();
  /** Track call stack for circular dependency detection. */
  private callStack = new Set<string>();

  async run(script: ScriptDefinition): Promise<ScriptResult> {
    const startedAt = this.deps.clock.now();
    const variables = new Map(Object.entries(script.env));
    const captures = new Map<string, CapturedRequest>();
    const artifacts = createArtifacts();
    const stepResults: StepResult[] = [];
    let failedAtStep: number | undefined;

    let globalStepIndex = 0;
    let nestedDepth = 0;

    // Validate proxy requirement before executing any steps
    if (script.proxy === "required") {
      const nc = this.deps.networkCapture;
      if (!nc || nc.constructor.name === "NullCapture") {
        return {
          scriptName: script.name,
          ok: false,
          steps: [],
          totalSteps: script.steps.length,
          passedSteps: 0,
          failedAtStep: 0,
          durationMs: 0,
          artifacts,
        };
      }
    }

    // runSteps — used by handle/branch/requires to execute steps in shared context
    const runSteps = async (
      steps: readonly ScriptStep[],
    ): Promise<CommandResult> => {
      nestedDepth++;
      try {
        for (const nestedStep of steps) {
          this.deps.signal?.throwIfAborted();
          const cmd = this.commandRegistry.get(nestedStep.command);
          if (!cmd) {
            return { ok: false, detail: `Unknown command: "${nestedStep.command}"` };
          }
          const myIndex = globalStepIndex++;
          const summary = summarizeStep(nestedStep);
          this.emitStep({
            type: "stepStarted",
            stepIndex: myIndex,
            command: nestedStep.command,
            summary: summary.text,
            tokens: summary.tokens,
            depth: nestedDepth,
          });
          const nestedCtx: CommandContext = {
            orchestra: this.deps.orchestra,
            clock: this.deps.clock,
            logger: this.deps.logger,
            variables,
            captures,
            networkCapture: this.deps.networkCapture ?? null,
            stepIndex: myIndex,
            script,
            artifacts,
            runSteps,
          };
          const stepStart = this.deps.clock.now();
          let result: CommandResult;
          try {
            result = await cmd.execute(nestedStep as never, nestedCtx);
          } catch (err) {
            result = {
              ok: false,
              detail: `Exception: ${(err as Error).message}`,
            };
          }
          const durationMs = this.deps.clock.now() - stepStart;
          this.emitStep({
            type: "stepCompleted",
            stepIndex: myIndex,
            command: nestedStep.command,
            summary: summary.text,
            tokens: summary.tokens,
            ok: result.ok,
            detail: result.detail,
            durationMs,
            depth: nestedDepth,
          });
          if (!result.ok) return result;
        }
        return { ok: true, detail: "completed" };
      } finally {
        nestedDepth--;
      }
    };

    // Execute required flows (dedup + circular detection + shared state)
    if (script.requires) {
      for (const reqPath of script.requires) {
        const absPath = resolve(reqPath);

        // Dedup — skip if already completed in this session
        if (this.completedRequires.has(absPath)) continue;

        // Circular detection
        if (this.callStack.has(absPath)) {
          return {
            scriptName: script.name,
            ok: false,
            steps: [{
              stepIndex: -1,
              command: "requires",
              ok: false,
              detail: `Circular dependency detected: ${reqPath}`,
              durationMs: 0,
            }],
            totalSteps: script.steps.length,
            passedSteps: 0,
            failedAtStep: 0,
            durationMs: this.deps.clock.now() - startedAt,
            artifacts,
          };
        }

        this.callStack.add(absPath);
        try {
          const yaml = readFileSync(absPath, "utf-8");
          const parentEnv = Object.fromEntries(variables);
          const reqScript = parseScript(yaml, parentEnv);

          // Execute required flow steps in shared context
          const reqResult = await runSteps(reqScript.steps);
          if (!reqResult.ok) {
            return {
              scriptName: script.name,
              ok: false,
              steps: [{
                stepIndex: -1,
                command: "requires",
                ok: false,
                detail: `Required flow "${reqPath}" failed: ${reqResult.detail}`,
                durationMs: 0,
              }],
              totalSteps: script.steps.length,
              passedSteps: 0,
              failedAtStep: 0,
              durationMs: this.deps.clock.now() - startedAt,
              artifacts,
            };
          }

          this.completedRequires.add(absPath);
        } finally {
          this.callStack.delete(absPath);
        }
      }
    }

    for (let i = 0; i < script.steps.length; i++) {
      this.deps.signal?.throwIfAborted();
      const step = script.steps[i]!;
      const cmd = this.commandRegistry.get(step.command);
      const myIndex = globalStepIndex++;

      if (!cmd) {
        stepResults.push({
          stepIndex: i,
          command: step.command,
          ok: false,
          detail: `Unknown command: "${step.command}"`,
          durationMs: 0,
        });
        failedAtStep = i;
        break;
      }

      const ctx: CommandContext = {
        orchestra: this.deps.orchestra,
        clock: this.deps.clock,
        logger: this.deps.logger,
        variables,
        captures,
        networkCapture: this.deps.networkCapture ?? null,
        stepIndex: i,
        script,
        artifacts,
        runSteps,
      };

      // Optional inter-step delay. Default 0 — Orchestra actions and
      // the wait primitives synchronize on `hierarchy()` state, so a
      // fixed pad is unnecessary for correctness. Scripts that need
      // a deliberate pause between steps (pacing a demo recording,
      // yielding to an external process) set `stepDelay` explicitly.
      const stepDelay = script.stepDelay ?? 0;
      if (i > 0 && stepDelay > 0 && step.command !== "sleep") {
        await this.deps.clock.sleep(stepDelay);
      }

      const summary = summarizeStep(step);
      const line = script._stepLines?.[i] || undefined;
      this.emitStep({
        type: "stepStarted",
        stepIndex: myIndex,
        command: step.command,
        summary: summary.text,
        tokens: summary.tokens,
        depth: 0,
        line,
      });
      const stepStart = this.deps.clock.now();
      try {
        const result = await cmd.execute(step as never, ctx);
        const durationMs = this.deps.clock.now() - stepStart;
        stepResults.push({
          stepIndex: i,
          command: step.command,
          ok: result.ok,
          detail: result.detail,
          durationMs,
        });
        this.emitStep({
          type: "stepCompleted",
          stepIndex: myIndex,
          command: step.command,
          summary: summary.text,
          tokens: summary.tokens,
          ok: result.ok,
          detail: result.detail,
          durationMs,
          depth: 0,
          line,
        });

        if (!result.ok) {
          failedAtStep = i;
          break;
        }
      } catch (err) {
        const durationMs = this.deps.clock.now() - stepStart;
        const detail = `Exception: ${(err as Error).message}`;
        stepResults.push({
          stepIndex: i,
          command: step.command,
          ok: false,
          detail,
          durationMs,
        });
        this.emitStep({
          type: "stepCompleted",
          stepIndex: myIndex,
          command: step.command,
          summary: summary.text,
          tokens: summary.tokens,
          ok: false,
          detail,
          durationMs,
          depth: 0,
          line,
        });
        failedAtStep = i;
        break;
      }
    }

    const durationMs = this.deps.clock.now() - startedAt;
    const passedSteps = stepResults.filter((s) => s.ok).length;

    return {
      scriptName: script.name,
      ok: failedAtStep === undefined,
      steps: stepResults,
      totalSteps: script.steps.length,
      passedSteps,
      failedAtStep,
      durationMs,
      artifacts,
    };
  }
}

function createArtifacts(): ScriptArtifacts {
  const screenshots: { label: string; data: Uint8Array }[] = [];
  return {
    addScreenshot(label: string, data: Uint8Array) {
      screenshots.push({ label, data });
    },
    getScreenshots() {
      return screenshots;
    },
  };
}
