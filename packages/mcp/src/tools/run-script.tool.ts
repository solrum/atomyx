import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";
import {
  parseScript,
  ScriptRunner,
  type ScriptResult,
} from "@atomyx/script";

const RunScriptArgs = z
  .object({
    script: z
      .string()
      .describe(
        "YAML test script content. Use the Atomyx YAML format with " +
          "appId, name, env, and steps.",
      ),
    env: z
      .record(z.string())
      .optional()
      .describe("Additional env variables to merge into the script."),
  })
  .strict();

/**
 * `run_script` — execute a YAML test script deterministically.
 *
 * The agent provides the full YAML script as a string. The tool
 * parses it, executes each step sequentially against the current
 * device, and returns a structured result with pass/fail per step.
 *
 * This is a different execution model from interactive tool calls:
 * the script runs to completion (or first failure) without agent
 * intervention. Use it for regression runs, CI integration, or
 * replaying recorded flows.
 */
export const runScriptTool = defineTool({
  name: "run_script",
  description:
    "Execute a YAML test script deterministically against the current device. " +
    "Each step runs sequentially; execution stops on the first failure. " +
    "Returns structured results with pass/fail per step. Use this for " +
    "regression runs or replaying recorded flows, not for exploratory testing.",
  inputSchema: RunScriptArgs,
  async execute(args, ctx): Promise<ScriptResult> {
    const orchestra = orchestraOrFail(ctx);
    const script = parseScript(args.script, args.env);
    const runner = new ScriptRunner({
      orchestra,
      clock: ctx.clock,
      logger: ctx.logger,
    });
    return runner.run(script);
  },
});
