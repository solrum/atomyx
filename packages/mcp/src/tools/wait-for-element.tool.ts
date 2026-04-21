import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";
import { SelectorSchema, compileSelectorInput } from "../selector-schema.js";
import {
  FindTimeoutError,
  AttrKeys,
  parseBounds,
  boundsCenter,
} from "@atomyx/driver";

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
 *
 * Response shape is intentionally identical to `find_element`
 * (plus `reason` / `elapsedMs` / `polls` on the failure path) so
 * agents can treat wait+find as a uniform "get element details"
 * pair: the poll-then-use-coordinates flow doesn't require a
 * second find_element call to compute the tap center.
 */
export const waitForElementTool = defineTool({
  name: "wait_for_element",
  description:
    "Poll the UI hierarchy until an element matching the selector appears, " +
    "or the timeout expires. Use after a tap that triggers navigation: " +
    "wait_for_element({selector: {text: 'Welcome'}, timeoutMs: 5000}). " +
    "Returns the same shape as find_element on success (id, text, label, " +
    "value, role, bounds, center, enabled, clickable) or " +
    "{found: false, reason, elapsedMs, polls} on timeout.",
  inputSchema: WaitForElementArgs,
  async execute(args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    const selector = compileSelectorInput(args.selector);
    try {
      const cursors = await orchestra.waitFor(selector, {
        timeoutMs: args.timeoutMs ?? 5000,
        pollIntervalMs: args.pollIntervalMs,
      });
      const cursor = cursors[0]!;
      const attrs = cursor.node.attributes;
      const bounds = parseBounds(attrs[AttrKeys.Bounds]);
      return {
        found: true,
        id: attrs[AttrKeys.Id],
        text: attrs[AttrKeys.Text],
        label: attrs[AttrKeys.Label],
        value: attrs[AttrKeys.Value],
        role: attrs[AttrKeys.Role],
        bounds,
        center: bounds ? boundsCenter(bounds) : null,
        enabled: cursor.node.enabled,
        clickable: cursor.node.clickable,
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
