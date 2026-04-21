/**
 * Prompt registry — a single ordered list of every methodology
 * prompt the default MCP server exposes via the `prompts/`
 * capability. Same registration model as `tools/index.ts`:
 * explicit, not auto-discovered.
 *
 * Adding a new prompt:
 *   1. Create `prompts/<name>.ts` exporting a `PromptDefinition`
 *      via `definePrompt({...})`.
 *   2. Append it to `DEFAULT_PROMPTS` below.
 *   3. Add a test in `prompts.test.ts`.
 *
 * Feature consumers can ship a different surface by passing a
 * custom `prompts` list to `createMcpServer({prompts: [...]})`.
 */

import type { PromptDefinition } from "./prompt-definition.js";
import { exploratoryPrompt } from "./exploratory.js";
import { regressionPrompt } from "./regression.js";
import { bugReproPrompt } from "./bug-repro.js";
import { playbookPrompt } from "./playbook.js";

export const DEFAULT_PROMPTS: readonly PromptDefinition[] = [
  playbookPrompt,
  exploratoryPrompt,
  regressionPrompt,
  bugReproPrompt,
];

export {
  exploratoryPrompt,
  regressionPrompt,
  bugReproPrompt,
  playbookPrompt,
};
export type { PromptDefinition, PromptArgument, PromptMessage } from "./prompt-definition.js";
export { definePrompt, interpolate } from "./prompt-definition.js";
