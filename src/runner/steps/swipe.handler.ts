import type { Step } from "../spec-schema.js";
import { buildResult, type StepContext, type StepHandler, type StepResult } from "./types.js";

type SwipeStep = Extract<Step, { swipe: any }>;

export class SwipeStepHandler implements StepHandler<SwipeStep> {
  readonly kind = "swipe";

  matches(step: Step): step is SwipeStep {
    return "swipe" in step;
  }

  async execute(step: SwipeStep, ctx: StepContext): Promise<StepResult> {
    const startedAt = Date.now();
    try {
      await ctx.controller.swipe(
        step.swipe.fromX,
        step.swipe.fromY,
        step.swipe.toX,
        step.swipe.toY,
        step.swipe.durationMs,
      );
      return buildResult(ctx, step, startedAt, "passed");
    } catch (err) {
      return buildResult(ctx, step, startedAt, "failed", err instanceof Error ? err.message : String(err));
    }
  }
}
