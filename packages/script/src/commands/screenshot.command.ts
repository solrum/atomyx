import { defineCommand } from "@atomyx/driver/script";
import type { ScreenshotStep } from "@atomyx/shared/script";

export const screenshotCommand = defineCommand<ScreenshotStep>({
  command: "screenshot",
  async execute(args, ctx) {
    const data = await ctx.orchestra.screenshot();
    const label = args.label ?? `step-${ctx.stepIndex + 1}`;
    ctx.artifacts.addScreenshot(label, data);
    return { ok: true, detail: `Screenshot saved as "${label}"` };
  },
});
