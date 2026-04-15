/**
 * Spec runner — orchestration only. Step execution is delegated to
 * the strategy registry in runner/steps. Bug rule evaluation is delegated
 * to a small inline applier (could be extracted later).
 */

import { readFileSync } from "node:fs";
import { load as loadYaml } from "js-yaml";

import type { DeviceController } from "../adapters/device-controller.port.js";
import { flattenText } from "../adapters/tree-diff.js";
import type { AtomyxContext } from "../runtime/atomyx-context.js";
import { specSchema, type BugRule, type Spec, type Step } from "./spec-schema.js";
import { stepHandlers, type StepResult } from "./steps/index.js";
import { resolveDeep } from "./var-resolver.js";

export type { StepResult } from "./steps/index.js";

export interface RunSummary {
  spec: string;
  status: "passed" | "failed" | "error";
  startedAt: number;
  finishedAt: number;
  durationMs: number;
  steps: StepResult[];
  verifyResult?: { ok: boolean; missing: string[]; forbidden: string[] };
  bugs: number;
  resultPath?: string;
}

export function loadSpec(path: string): Spec {
  const raw = readFileSync(path, "utf8");
  const parsed = loadYaml(raw);
  return specSchema.parse(parsed);
}

async function executeStep(ctl: DeviceController, step: Step, index: number): Promise<StepResult> {
  for (const handler of stepHandlers) {
    if (handler.matches(step)) {
      return handler.execute(step, { controller: ctl, index });
    }
  }
  return {
    index,
    kind: "unknown",
    status: "failed",
    durationMs: 0,
    error: `no handler for step: ${JSON.stringify(step)}`,
  };
}

function applyBugRules(
  ctx: AtomyxContext,
  rules: BugRule[],
  step: StepResult,
  kind: "step" | "verify",
  verifyResult?: { missing: string[]; forbidden: string[] },
) {
  for (const rule of rules) {
    if (rule.if === "step_failed" && kind === "step" && step.status === "failed") {
      ctx.results.reportBug({
        severity: rule.severity,
        title: `Step ${step.index} (${step.kind}) failed`,
        description: step.error,
        context: step.context,
      });
    }
    if (rule.if === "verify_failed" && kind === "verify" && verifyResult) {
      ctx.results.reportBug({
        severity: rule.severity,
        title: "Verify failed",
        description: `missing=${JSON.stringify(verifyResult.missing)} forbidden=${JSON.stringify(verifyResult.forbidden)}`,
      });
    }
    if (rule.if === "timeout" && step.error?.includes("timed out")) {
      ctx.results.reportBug({
        severity: rule.severity,
        title: `Timeout at step ${step.index}`,
        description: step.error,
      });
    }
  }
}

export async function runSpec(
  ctx: AtomyxContext,
  ctl: DeviceController,
  spec: Spec,
  specPath: string,
): Promise<RunSummary> {
  const startedAt = Date.now();
  const resolved = resolveDeep(spec, {
    data: spec.data,
    env: process.env as Record<string, string | undefined>,
  });

  ctx.history.start();
  const run = ctx.results.startRun({
    name: resolved.name,
    source: "scripted",
    deviceId: ctl.deviceId,
    platform: ctl.platform,
    meta: { specPath },
  });

  const stepResults: StepResult[] = [];
  let overallStatus: "passed" | "failed" | "error" = "passed";
  const rules = resolved.bug_rules ?? [];

  const allSteps: Step[] = [...(resolved.setup ?? []), ...resolved.steps];

  let i = 0;
  for (const step of allSteps) {
    const r = await executeStep(ctl, step, i++);
    stepResults.push(r);
    applyBugRules(ctx, rules, r, "step");
    if (r.status === "failed") overallStatus = "failed";
  }

  // Final verify block
  let verifyResult: RunSummary["verifyResult"];
  if (resolved.verify) {
    try {
      const tree = await ctl.getUiTree();
      const haystack = flattenText(tree).join(" \u00a7 ").toLowerCase();
      const missing = (resolved.verify.mustContain ?? []).filter((s) => !haystack.includes(s.toLowerCase()));
      const forbidden = (resolved.verify.mustNotContain ?? []).filter((s) => haystack.includes(s.toLowerCase()));
      verifyResult = { ok: missing.length === 0 && forbidden.length === 0, missing, forbidden };
      if (!verifyResult.ok) {
        overallStatus = "failed";
        applyBugRules(
          ctx,
          rules,
          { index: -1, kind: "verify", status: "failed", durationMs: 0 } as StepResult,
          "verify",
          { missing, forbidden },
        );
      }
    } catch {
      verifyResult = { ok: false, missing: [], forbidden: [] };
      overallStatus = "error";
    }
  }

  // Teardown — best effort
  for (const step of resolved.teardown ?? []) {
    await executeStep(ctl, step, i++).catch(() => {});
  }

  ctx.results.finishRun(overallStatus);
  const resultPath = ctx.results.persistLocal();

  return {
    spec: resolved.name,
    status: overallStatus,
    startedAt,
    finishedAt: Date.now(),
    durationMs: Date.now() - startedAt,
    steps: stepResults,
    verifyResult,
    bugs: run.bugs.length,
    resultPath: resultPath ?? undefined,
  };
}
