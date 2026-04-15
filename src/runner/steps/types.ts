import type { DeviceController, Selector } from "../../adapters/device-controller.port.js";
import type { Step } from "../spec-schema.js";

export interface StepResult {
  index: number;
  id?: string;
  kind: string;
  status: "passed" | "failed" | "skipped";
  durationMs: number;
  error?: string;
  context?: Record<string, unknown>;
}

export interface StepContext {
  controller: DeviceController;
  index: number;
}

export interface StepHandler<T extends Step = Step> {
  readonly kind: string;
  matches(step: Step): step is T;
  execute(step: T, ctx: StepContext): Promise<StepResult>;
}

/** Helper to convert any criteria-shaped object to a Selector. */
export function toSelector(criteria: Record<string, unknown>): Selector {
  return {
    resourceId: criteria.resourceId as string | undefined,
    contentDesc: criteria.contentDesc as string | undefined,
    text: criteria.text as string | undefined,
    textContains: criteria.textContains as string | undefined,
    hint: criteria.hint as string | undefined,
    nth: criteria.nth as number | undefined,
  };
}

/** Helper builder for StepResult so handlers don't repeat boilerplate. */
export function buildResult(
  ctx: StepContext,
  step: Step,
  startedAt: number,
  status: "passed" | "failed",
  error?: string,
  context?: Record<string, unknown>,
): StepResult {
  return {
    index: ctx.index,
    id: (step as { id?: string }).id,
    kind: stepKind(step),
    status,
    durationMs: Date.now() - startedAt,
    error,
    context,
  };
}

export function stepKind(step: Step): string {
  if ("launch" in step) return "launch";
  if ("tap" in step) return "tap";
  if ("input" in step) return "input";
  if ("swipe" in step) return "swipe";
  if ("press_key" in step) return "press_key";
  if ("wait_for_idle" in step) return "wait_for_idle";
  if ("wait_for" in step) return "wait_for";
  if ("assert" in step) return "assert";
  if ("sleep" in step) return "sleep";
  return "unknown";
}
