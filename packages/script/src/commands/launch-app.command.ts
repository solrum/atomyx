import { defineCommand } from "@atomyx/driver/script";
import type { LaunchAppStep } from "@atomyx/shared/script";

export const launchAppCommand = defineCommand<LaunchAppStep>({
  command: "launchApp",
  async execute(_args, ctx) {
    const appId = ctx.script.appId;
    if (!appId) {
      return { ok: false, detail: "No appId specified in script config" };
    }
    await ctx.orchestra.launchApp(appId);
    return { ok: true, detail: `Launched ${appId}` };
  },
});
