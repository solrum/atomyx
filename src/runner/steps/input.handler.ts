import type { Step } from "../spec-schema.js";
import { buildResult, toSelector, type StepContext, type StepHandler, type StepResult } from "./types.js";

type InputStep = Extract<Step, { input: any }>;

export class InputStepHandler implements StepHandler<InputStep> {
  readonly kind = "input";

  matches(step: Step): step is InputStep {
    return "input" in step;
  }

  async execute(step: InputStep, ctx: StepContext): Promise<StepResult> {
    const startedAt = Date.now();
    const selector = toSelector(step.input.find as Record<string, unknown>);
    try {
      const focused = await ctx.controller.tap(selector);
      if (!focused.ok) {
        return buildResult(ctx, step, startedAt, "failed", `input(focus): ${focused.reason}`, { selector });
      }
      await new Promise((r) => setTimeout(r, 150));
      const set = await ctx.controller.inputText(selector, step.input.text);
      if (!set.ok) {
        return buildResult(ctx, step, startedAt, "failed", `input(set_text): ${set.reason}`, { selector });
      }
      return buildResult(ctx, step, startedAt, "passed", undefined, { selector, text: step.input.text });
    } catch (err) {
      return buildResult(ctx, step, startedAt, "failed", err instanceof Error ? err.message : String(err), { selector });
    }
  }
}
