import type { Step } from "../spec-schema.js";
import { buildResult, toSelector, type StepContext, type StepHandler, type StepResult } from "./types.js";

type TapStep = Extract<Step, { tap: any }>;

export class TapStepHandler implements StepHandler<TapStep> {
  readonly kind = "tap";

  matches(step: Step): step is TapStep {
    return "tap" in step;
  }

  async execute(step: TapStep, ctx: StepContext): Promise<StepResult> {
    const startedAt = Date.now();
    const selector = toSelector(step.tap as Record<string, unknown>);
    try {
      const r = await ctx.controller.tap(selector);
      if (!r.ok) return buildResult(ctx, step, startedAt, "failed", `tap: ${r.reason}`, { selector });
      return buildResult(ctx, step, startedAt, "passed", undefined, { selector, reason: r.reason });
    } catch (err) {
      return buildResult(ctx, step, startedAt, "failed", err instanceof Error ? err.message : String(err), { selector });
    }
  }
}
