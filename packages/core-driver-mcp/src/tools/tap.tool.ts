import { z } from "zod";
import { defineTool } from "../tool-definition.js";
import { SelectorSchema, compileSelectorInput } from "../selector-schema.js";

const TapArgs = z
  .union([
    z.object({
      selector: SelectorSchema,
    }),
    z.object({
      x: z.number(),
      y: z.number(),
    }),
  ])
  .describe(
    "Either {selector} for selector-based tap (with auto scroll-into-view " +
      "and obscurement check), or {x,y} for raw coordinate tap.",
  );

/**
 * `tap` — the most-used action tool. Routes to either the
 * Orchestra selector pipeline (compile → scroll-into-view →
 * obscurement → tap) or the raw coordinate primitive based on
 * which arg shape was passed.
 *
 * Returns `ActionResult`-shaped output so the agent can read
 * `ok`, `reason`, `obscurer`, `resolvedBy` and react.
 */
export const tapTool = defineTool({
  name: "tap",
  description:
    "Tap an element. Pass {selector: {...}} to find and tap the matching " +
    "element with automatic scroll-into-view and obscurement detection, OR " +
    "pass {x, y} to tap raw coordinates (use when you have a center point " +
    "from get_ui_tree or find_element). Returns ok=true on success, or " +
    "ok=false with a reason and optional obscurer info on failure.",
  inputSchema: TapArgs,
  async execute(args, ctx) {
    if ("selector" in args) {
      const selector = compileSelectorInput(args.selector);
      return ctx.orchestra.tap(selector);
    }
    await ctx.orchestra.tapAt({ x: args.x, y: args.y });
    return { ok: true, detail: `tapped at (${args.x},${args.y})` };
  },
});
