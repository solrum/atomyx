import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";

const PressKeyArgs = z
  .object({
    key: z.string().describe("Key name: back, home, enter, tab, escape, delete, etc."),
  })
  .strict();

/**
 * `press_key` — system / navigation key. Returns ActionResult
 * shape because iOS may legitimately fail (no system back),
 * and the agent needs to know to fall back to an on-screen
 * affordance.
 */
export const pressKeyTool = defineTool({
  name: "press_key",
  description:
    "Press a system or navigation key (back, home, enter, etc.). On iOS, " +
    "'back' may return ok=false because there's no system back primitive — " +
    "the response includes a hint to find a Cancel/Close button instead.",
  inputSchema: PressKeyArgs,
  async execute(args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    return orchestra.pressKey(args.key);
  },
});
