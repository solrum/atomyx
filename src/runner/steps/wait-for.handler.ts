import type { Step } from "../spec-schema.js";
import { buildResult, toSelector, type StepContext, type StepHandler, type StepResult } from "./types.js";

type WaitForStep = Extract<Step, { wait_for: any }>;

export class WaitForStepHandler implements StepHandler<WaitForStep> {
  readonly kind = "wait_for";

  matches(step: Step): step is WaitForStep {
    return "wait_for" in step;
  }

  async execute(step: WaitForStep, ctx: StepContext): Promise<StepResult> {
    const startedAt = Date.now();
    const { timeoutMs = 5000, ...criteria } = step.wait_for;
    const selector = toSelector(criteria as Record<string, unknown>);
    try {
      while (Date.now() - startedAt < timeoutMs) {
        const r = await ctx.controller.resolveSelector(selector);
        if (r.found) return buildResult(ctx, step, startedAt, "passed", undefined, { selector });
        await new Promise((r) => setTimeout(r, 300));
      }
      return buildResult(ctx, step, startedAt, "failed", `wait_for: element not found within ${timeoutMs}ms`, { selector });
    } catch (err) {
      return buildResult(ctx, step, startedAt, "failed", err instanceof Error ? err.message : String(err), { selector });
    }
  }
}
