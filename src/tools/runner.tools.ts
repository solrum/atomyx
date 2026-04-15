import { resolve as resolvePath } from "node:path";
import { runExploration } from "../explorer/agent-loop.js";
import { loadSpec, runSpec } from "../runner/spec-runner.js";
import { requireController } from "../runtime/adet-context.js";
import type { ToolCategory } from "./tool-factory.js";

export const registerRunnerTools: ToolCategory = (factory, ctx) => {
  factory.register({
    name: "start_exploration",
    description:
      "Run an autonomous exploratory test session. Spawns an in-process Claude API agent that explores the app via the same adet tools and reports bugs. " +
      "Requires ANTHROPIC_API_KEY env var. Returns summary with bugs, findings, token usage.",
    inputSchema: {
      type: "object",
      required: ["app", "goal"],
      properties: {
        app: { type: "string", description: "Target package name" },
        goal: { type: "string", description: "What to test (e.g. 'find login validation bugs')" },
        maxSteps: { type: "number", default: 30 },
        earlyExitOnCritical: { type: "boolean", default: false },
      },
    },
    handler: async (args: { app: string; goal: string; maxSteps?: number; earlyExitOnCritical?: boolean }) => {
      const ctl = requireController(ctx);
      return runExploration(ctl, {
        app: args.app,
        goal: args.goal,
        maxSteps: args.maxSteps ?? 30,
        earlyExitOnCritical: args.earlyExitOnCritical ?? false,
      });
    },
  });

  factory.register({
    name: "run_test_spec",
    description:
      "Load and execute a YAML test spec against the currently selected device. " +
      "Returns step-by-step results, verify outcome, and bug count. " +
      "Local result JSON saved to /tmp/adet-results.",
    inputSchema: {
      type: "object",
      required: ["specPath"],
      properties: {
        specPath: { type: "string", description: "Absolute or repo-relative path to a .yaml spec file" },
      },
    },
    handler: async (args: { specPath: string }) => {
      const ctl = requireController(ctx);
      const absPath = resolvePath(args.specPath);
      const spec = loadSpec(absPath);
      return runSpec(ctx, ctl, spec, absPath);
    },
  });
};
