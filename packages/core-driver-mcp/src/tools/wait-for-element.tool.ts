import { z } from "zod";
import { defineTool } from "../tool-definition.js";
import { SelectorSchema, compileSelectorInput } from "../selector-schema.js";
import { FindTimeoutError, AttrKeys } from "@atomyx/core-driver";

const WaitForElementArgs = z
  .object({
    selector: SelectorSchema,
    timeoutMs: z.number().int().positive().optional(),
    pollIntervalMs: z.number().int().positive().optional(),
  })
  .strict();

/**
 * `wait_for_element` — poll until an element appears or the
 * deadline expires. Used after navigation actions to assert
 * "I expect screen X to load within N seconds". Returns ok=false
 * with a timeout reason instead of throwing — the agent is
 * supposed to react to the result, not crash on it.
 */
export const waitForElementTool = defineTool({
  name: "wait_for_element",
  description:
    "Poll the UI hierarchy until an element matching the selector appears, " +
    "or the timeout expires. Use after a tap that triggers navigation: " +
    "wait_for_element({selector: {text: 'Welcome'}, timeoutMs: 5000}). " +
    "Returns {found: true, ...elementInfo} or {found: false, reason}.",
  inputSchema: WaitForElementArgs,
  async execute(args, ctx) {
    const selector = compileSelectorInput(args.selector);
    try {
      const cursors = await ctx.orchestra.waitFor(selector, {
        timeoutMs: args.timeoutMs ?? 5000,
        pollIntervalMs: args.pollIntervalMs,
      });
      const cursor = cursors[0]!;
      const attrs = cursor.node.attributes;
      return {
        found: true,
        id: attrs[AttrKeys.Id],
        text: attrs[AttrKeys.Text],
        label: attrs[AttrKeys.Label],
        role: attrs[AttrKeys.Role],
        bounds: attrs[AttrKeys.Bounds],
      };
    } catch (err) {
      if (err instanceof FindTimeoutError) {
        return {
          found: false,
          reason: err.message,
          elapsedMs: err.elapsedMs,
          polls: err.pollCount,
        };
      }
      throw err;
    }
  },
});
