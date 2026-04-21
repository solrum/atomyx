import type { Orchestra } from "../orchestra/orchestra.js";
import type { Clock } from "@atomyx/core/infra";
import type { Logger } from "@atomyx/core/infra";
import type {
  CapturedRequest,
  NetworkCapture,
  ScriptArtifacts,
  ScriptDefinition,
  ScriptStep,
} from "@atomyx/shared/script";

/**
 * Context passed to every command during script execution.
 * Mirrors `ToolContext` from core-driver-mcp but scoped to
 * the script engine's needs.
 */
export interface CommandContext {
  /** The Orchestra instance bound to the current device. */
  readonly orchestra: Orchestra;
  /** Clock for timing and sleep. */
  readonly clock: Clock;
  /** Logger for debug output. */
  readonly logger: Logger;
  /**
   * Runtime variables. Starts with env vars from script config.
   * `extract` command adds variables at runtime (API response data).
   * Available via `${name}` in subsequent steps.
   */
  readonly variables: Map<string, string>;
  /**
   * Mutable map of captured API responses, keyed by the
   * variable name from `capture ... as: varName`. Commands
   * like `assertApi` read from this.
   */
  readonly captures: Map<string, CapturedRequest>;
  /** Network capture adapter (null when no proxy configured). */
  readonly networkCapture: NetworkCapture | null;
  /** Zero-based index of the current step. */
  readonly stepIndex: number;
  /** The full script definition (for appId, name access). */
  readonly script: ScriptDefinition;
  /** Artifact collector for screenshots and evidence. */
  readonly artifacts: ScriptArtifacts;
  /**
   * Execute nested steps (used by handle/branch do blocks).
   * Provided by the runner — commands should not implement this.
   */
  readonly runSteps: (steps: readonly ScriptStep[]) => Promise<CommandResult>;
}

/**
 * Result of executing a single script command.
 */
export interface CommandResult {
  /** Whether the command succeeded. */
  readonly ok: boolean;
  /** Human-readable detail (reason on failure, info on success). */
  readonly detail?: string;
}

/**
 * Definition of a script command. Each command is one file in
 * `commands/` + one entry in `DEFAULT_COMMANDS`.
 *
 * Adding a new command:
 * 1. Create `commands/my-command.command.ts` with `defineCommand`.
 * 2. Append to `DEFAULT_COMMANDS` in `commands/index.ts`.
 */
export interface CommandDefinition<TArgs = unknown> {
  /** Command name — must match `ScriptStep.command`. */
  readonly command: string;
  /** Execute the command against the current device. */
  execute(args: TArgs, ctx: CommandContext): Promise<CommandResult>;
}

/**
 * Identity function for type inference — returns its argument
 * unchanged but forces TypeScript to infer TArgs from the
 * execute callback. Same pattern as `defineTool` in
 * core-driver-mcp.
 */
export function defineCommand<TArgs>(
  def: CommandDefinition<TArgs>,
): CommandDefinition<TArgs> {
  return def;
}

/** Type-erased command for use in registry arrays. */
export type AnyCommandDefinition = CommandDefinition<unknown>;
