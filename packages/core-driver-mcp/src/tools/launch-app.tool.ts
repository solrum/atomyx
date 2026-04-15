import { z } from "zod";
import { defineTool } from "../tool-definition.js";

const LaunchAppArgs = z
  .object({
    appId: z.string().describe("Bundle id (iOS) or package name (Android)."),
  })
  .strict();

export const launchAppTool = defineTool({
  name: "launch_app",
  description:
    "Launch an app on the connected device. Pass the bundle id (iOS) or " +
    "package name (Android). The driver brings the app to the foreground; " +
    "subsequent tool calls operate on its UI.",
  inputSchema: LaunchAppArgs,
  async execute(args, ctx) {
    await ctx.orchestra.launchApp(args.appId);
    return { ok: true, appId: args.appId };
  },
});
