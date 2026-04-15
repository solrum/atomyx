import type { Step } from "../spec-schema.js";
import { buildResult, type StepContext, type StepHandler, type StepResult } from "./types.js";

type PressKeyStep = Extract<Step, { press_key: "back" | "home" | "enter" }>;

export class PressKeyStepHandler implements StepHandler<PressKeyStep> {
  readonly kind = "press_key";

  matches(step: Step): step is PressKeyStep {
    return "press_key" in step;
  }

  async execute(step: PressKeyStep, ctx: StepContext): Promise<StepResult> {
    const startedAt = Date.now();
    try {
      await ctx.controller.pressKey(step.press_key);
      return buildResult(ctx, step, startedAt, "passed");
    } catch (err) {
      return buildResult(ctx, step, startedAt, "failed", err instanceof Error ? err.message : String(err));
    }
  }
}
