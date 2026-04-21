import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineCommand } from "@atomyx/driver/script";
import type { RunFlowStep } from "@atomyx/shared/script";
import { parseScript } from "../parser/yaml-parser.js";

export const runFlowCommand = defineCommand<RunFlowStep>({
  command: "runFlow",
  async execute(args, ctx) {
    // Resolve file path relative to the current working directory
    const filePath = resolve(args.file);

    let yamlContent: string;
    try {
      yamlContent = readFileSync(filePath, "utf-8");
    } catch {
      return {
        ok: false,
        detail: `Sub-flow file not found: ${filePath}`,
      };
    }

    // Merge parent variables + sub-flow env overrides
    const parentEnv: Record<string, string> = {};
    for (const [k, v] of ctx.variables) {
      parentEnv[k] = v;
    }
    const mergedEnv = { ...parentEnv, ...(args.env ?? {}) };

    const subScript = parseScript(yamlContent, mergedEnv);

    // Execute sub-flow steps in current context
    const result = await ctx.runSteps(subScript.steps);
    if (!result.ok) {
      return {
        ok: false,
        detail: `Sub-flow "${subScript.name}" failed: ${result.detail}`,
      };
    }

    return {
      ok: true,
      detail: `Sub-flow "${subScript.name}" completed (${subScript.steps.length} steps)`,
    };
  },
});
