import { flattenText } from "../../adapters/tree-diff.js";
import type { Step } from "../spec-schema.js";
import { buildResult, type StepContext, type StepHandler, type StepResult } from "./types.js";

type AssertStep = Extract<Step, { assert: any }>;

export class AssertStepHandler implements StepHandler<AssertStep> {
  readonly kind = "assert";

  matches(step: Step): step is AssertStep {
    return "assert" in step;
  }

  async execute(step: AssertStep, ctx: StepContext): Promise<StepResult> {
    const startedAt = Date.now();
    try {
      const tree = await ctx.controller.getUiTree();
      const haystack = flattenText(tree).join(" \u00a7 ").toLowerCase();
      const missing: string[] = [];
      const forbidden: string[] = [];
      for (const s of step.assert.mustContain ?? []) {
        if (!haystack.includes(s.toLowerCase())) missing.push(s);
      }
      for (const s of step.assert.mustNotContain ?? []) {
        if (haystack.includes(s.toLowerCase())) forbidden.push(s);
      }
      if (missing.length === 0 && forbidden.length === 0) {
        return buildResult(ctx, step, startedAt, "passed");
      }
      return buildResult(
        ctx,
        step,
        startedAt,
        "failed",
        `assert failed: missing=${JSON.stringify(missing)} forbidden=${JSON.stringify(forbidden)}`,
        { missing, forbidden },
      );
    } catch (err) {
      return buildResult(ctx, step, startedAt, "failed", err instanceof Error ? err.message : String(err));
    }
  }
}
