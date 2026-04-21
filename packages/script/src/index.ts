/**
 * @atomyx/script — YML test script engine.
 *
 * Parses Atomyx YAML test scripts and executes them
 * deterministically via Orchestra. No LLM / agent dependency —
 * works standalone in any consumer (CLI, CI, embedded host).
 *
 * Public API:
 *   - parseScript(yaml)   → ScriptDefinition
 *   - ScriptRunner.run()  → ScriptResult
 *   - DEFAULT_COMMANDS    → built-in command registry
 */

export { parseScript } from "./parser/index.js";
export { ScriptRunner } from "./runner/index.js";
export type { ScriptRunnerDeps, ScriptResult, StepResult } from "./runner/index.js";
export { DEFAULT_COMMANDS } from "./commands/index.js";
export {
  createCapture,
  registerCaptureAdapter,
  NullCapture,
  FileCapture,
} from "./network/index.js";
