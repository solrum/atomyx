import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";

const LaunchAppArgs = z
  .object({
    appId: z
      .string()
      .describe(
        "Cross-platform app identifier (bundle id / package name). " +
          "Use the string reported by `list_apps`.",
      ),
  })
  .strict();

export const launchAppTool = defineTool({
  name: "launch_app",
  description:
    "Launch an app on the connected device by its app identifier. The " +
    "driver brings the app to the foreground; subsequent tool calls " +
    "operate on its UI. Requires an active device — call `select_device` " +
    "first. Use `list_apps` to discover available identifiers.",
  inputSchema: LaunchAppArgs,
  async execute(args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    await orchestra.launchApp(args.appId, undefined, { signal: ctx.signal });
    return { ok: true, appId: args.appId };
  },
});
