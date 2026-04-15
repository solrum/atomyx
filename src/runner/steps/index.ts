import { AssertStepHandler } from "./assert.handler.js";
import { InputStepHandler } from "./input.handler.js";
import { LaunchStepHandler } from "./launch.handler.js";
import { PressKeyStepHandler } from "./press-key.handler.js";
import { SleepStepHandler } from "./sleep.handler.js";
import { SwipeStepHandler } from "./swipe.handler.js";
import { TapStepHandler } from "./tap.handler.js";
import { WaitForIdleStepHandler } from "./wait-for-idle.handler.js";
import { WaitForStepHandler } from "./wait-for.handler.js";
import type { StepHandler } from "./types.js";

/**
 * Step handler registry. Adding a new step type:
 *   1. Define schema in spec-schema.ts
 *   2. Create handler class in this folder
 *   3. Register here
 *   No core file needs to change.
 */
export const stepHandlers: StepHandler[] = [
  new LaunchStepHandler(),
  new TapStepHandler(),
  new InputStepHandler(),
  new SwipeStepHandler(),
  new PressKeyStepHandler(),
  new WaitForIdleStepHandler(),
  new WaitForStepHandler(),
  new AssertStepHandler(),
  new SleepStepHandler(),
];

export function findHandler(step: import("../spec-schema.js").Step): StepHandler | undefined {
  return stepHandlers.find((h) => h.matches(step));
}

export type { StepHandler, StepContext, StepResult } from "./types.js";
