import type { ScriptStep } from "./script-step.js";

/**
 * Parsed, validated representation of a YAML test script.
 * Canonical in-memory shape — parsers (YAML, JSON, programmatic)
 * produce this; runners consume it.
 *
 * The shape is intentionally serializable (no functions, no
 * RegExp) so it can travel across process boundaries — HTTP APIs,
 * IPC, remote runners, etc.
 */
export interface ScriptDefinition {
  /**
   * Script format identifier. Runner uses this to select the
   * correct parser/behavior.
   *
   * Format: `"atomyx/v1"`, `"atomyx/v2"`, etc.
   * Defaults to `"atomyx/v1"` if omitted.
   */
  readonly format?: string;
  /** Bundle id (iOS) or package name (Android). */
  readonly appId: string;
  /** Human-readable script name for reporting. */
  readonly name: string;
  /** Human-readable description of what this script tests. */
  readonly description?: string;
  /** Preconditions that must be true before running. */
  readonly precondition?: string;
  /** Tags for filtering and organization. */
  readonly tags?: readonly string[];
  /** Variables available via `${varName}` in step values. */
  readonly env: Readonly<Record<string, string>>;
  /**
   * Whether the script requires a network capture proxy.
   * `"required"` → runner validates proxy before executing any step.
   * `"optional"` (default) → capture commands fail individually if no proxy.
   */
  readonly proxy?: "required" | "optional";
  /**
   * Flow files to run before this script. Paths relative to CWD.
   * If any required flow fails, this script is skipped.
   */
  readonly requires?: readonly string[];
  /**
   * Delay in ms between each step. Gives UI time to settle after
   * actions (animations, keyboard, network). Default: 500ms.
   * Set to 0 for maximum speed (unit tests with MockDriver).
   */
  readonly stepDelay?: number;
  /** Ordered list of steps to execute. */
  readonly steps: readonly ScriptStep[];
}
