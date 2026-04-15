import type { Step } from "../spec-schema.js";
import { buildResult, type StepContext, type StepHandler, type StepResult } from "./types.js";

type SleepStep = Extract<Step, { sleep: number }>;

export class SleepStepHandler implements StepHandler<SleepStep> {
  readonly kind = "sleep";

  matches(step: Step): step is SleepStep {
    return "sleep" in step;
  }

  async execute(step: SleepStep, ctx: StepContext): Promise<StepResult> {
    const startedAt = Date.now();
    await new Promise((r) => setTimeout(r, step.sleep));
    return buildResult(ctx, step, startedAt, "passed");
  }
}
