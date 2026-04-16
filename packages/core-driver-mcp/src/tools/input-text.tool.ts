import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";
import { SelectorSchema, compileSelectorInput } from "../selector-schema.js";

const InputTextArgs = z
  .object({
    selector: SelectorSchema.optional(),
    x: z.number().optional(),
    y: z.number().optional(),
    text: z.string(),
    clearFirst: z
      .boolean()
      .optional()
      .describe(
        "Erase existing content before typing. Default true. Set false to append.",
      ),
  })
  .strict()
  .refine(
    (a) => a.selector !== undefined || (a.x !== undefined && a.y !== undefined),
    "must provide either selector or x+y",
  );

/**
 * `input_text` — type into a field. Selector path runs the
 * full Orchestra pipeline (scroll-into-view, obscurement, tap-
 * to-focus, optional clear, type). Coordinate path skips
 * scroll/obscurement and just taps + types — the agent has
 * already decided where the field is.
 */
export const inputTextTool = defineTool({
  name: "input_text",
  description:
    "Type text into an input field. Pass either {selector, text} for full " +
    "pipeline (find + scroll-into-view + tap-to-focus + optional clear + " +
    "type) or {x, y, text} for coordinate-based input. Set " +
    "clearFirst=false to append instead of overwrite (default clears).",
  inputSchema: InputTextArgs,
  async execute(args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    if (args.selector) {
      const selector = compileSelectorInput(args.selector);
      return orchestra.inputText(selector, args.text, {
        clearFirst: args.clearFirst,
      });
    }
    // Coordinate path: tap to focus, then type. No selector
    // pipeline — caller already knows where the field is.
    await orchestra.tapAt({ x: args.x!, y: args.y! });
    if (args.clearFirst !== false) {
      // Try to clear via Orchestra's eraseText primitive when
      // the driver supports it. Otherwise fall through silently.
      try {
        await orchestra.eraseText(999);
      } catch {
        // Driver lacks canEraseText — caller may need to clear manually.
      }
    }
    await orchestra.typeText(args.text);
    return {
      ok: true,
      detail: `typed ${args.text.length} char(s) at (${args.x},${args.y})`,
    };
  },
});
