import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";

const PressKeyArgs = z
  .object({
    key: z.string().describe("Key name: back, home, enter, tab, escape, delete, etc."),
  })
  .strict();

/**
 * `press_key` — system / navigation key. Returns ActionResult shape
 * so the agent can react when a key isn't supported on the current
 * platform (e.g. no system back button on some devices).
 */
export const pressKeyTool = defineTool({
  name: "press_key",
  description:
    "Press a system or navigation key (back, home, enter, etc.). Some " +
    "keys may return ok=false when the platform has no equivalent " +
    "primitive; the response hint explains the recommended on-screen " +
    "fallback (e.g. tap a Cancel / Close button).",
  inputSchema: PressKeyArgs,
  async execute(args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    return orchestra.pressKey(args.key);
  },
});
