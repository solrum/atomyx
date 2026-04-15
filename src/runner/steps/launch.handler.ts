import type { Step } from "../spec-schema.js";
import { buildResult, type StepContext, type StepHandler, type StepResult } from "./types.js";

type LaunchStep = Extract<Step, { launch: string }>;

export class LaunchStepHandler implements StepHandler<LaunchStep> {
  readonly kind = "launch";

  matches(step: Step): step is LaunchStep {
    return "launch" in step;
  }

  async execute(step: LaunchStep, ctx: StepContext): Promise<StepResult> {
    const startedAt = Date.now();
    try {
      await ctx.controller.launchApp(step.launch);
      return buildResult(ctx, step, startedAt, "passed");
    } catch (err) {
      return buildResult(ctx, step, startedAt, "failed", err instanceof Error ? err.message : String(err));
    }
  }
}
