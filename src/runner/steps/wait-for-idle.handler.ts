import { treesEqual } from "../../adapters/tree-diff.js";
import type { Step } from "../spec-schema.js";
import { buildResult, type StepContext, type StepHandler, type StepResult } from "./types.js";

type WaitForIdleStep = Extract<Step, { wait_for_idle: any }>;

export class WaitForIdleStepHandler implements StepHandler<WaitForIdleStep> {
  readonly kind = "wait_for_idle";

  matches(step: Step): step is WaitForIdleStep {
    return "wait_for_idle" in step;
  }

  async execute(step: WaitForIdleStep, ctx: StepContext): Promise<StepResult> {
    const startedAt = Date.now();
    const cfg = step.wait_for_idle ?? {};
    const timeoutMs = cfg.timeoutMs ?? 5000;
    const idleMs = cfg.idleMs ?? 800;
    const intervalMs = 300;

    try {
      let lastTree = await ctx.controller.getUiTree();
      let lastChange = Date.now();
      while (Date.now() - startedAt < timeoutMs) {
        await new Promise((r) => setTimeout(r, intervalMs));
        const next = await ctx.controller.getUiTree();
        if (!treesEqual(lastTree, next)) {
          lastTree = next;
          lastChange = Date.now();
        } else if (Date.now() - lastChange >= idleMs) {
          return buildResult(ctx, step, startedAt, "passed", undefined, { idleAfterMs: Date.now() - startedAt });
        }
      }
      return buildResult(ctx, step, startedAt, "failed", `wait_for_idle: timed out after ${timeoutMs}ms`);
    } catch (err) {
      return buildResult(ctx, step, startedAt, "failed", err instanceof Error ? err.message : String(err));
    }
  }
}
