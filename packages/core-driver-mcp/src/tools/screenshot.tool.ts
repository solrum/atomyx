import { z } from "zod";
import { defineTool } from "../tool-definition.js";

const ScreenshotArgs = z.object({}).strict();

/**
 * `screenshot` — base64-encoded PNG. The MCP server forwards
 * the bytes to the agent which can then render it inline.
 * Sized minimally because base64 doubles the payload — caller
 * may want to downsample on the host side later if size becomes
 * a problem.
 */
export const screenshotTool = defineTool({
  name: "screenshot",
  description:
    "Capture a screenshot of the current screen as a base64-encoded PNG. " +
    "Useful for visual debugging when the UI tree alone is insufficient " +
    "(e.g. custom-rendered Flutter / canvas content).",
  inputSchema: ScreenshotArgs,
  async execute(_args, ctx) {
    const bytes = await ctx.orchestra.screenshot();
    return {
      base64: Buffer.from(bytes).toString("base64"),
      format: "png" as const,
      sizeBytes: bytes.length,
    };
  },
});
