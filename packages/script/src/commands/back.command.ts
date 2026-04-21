import { defineCommand } from "@atomyx/driver/script";
import type { BackStep } from "@atomyx/shared/script";

export const backCommand = defineCommand<BackStep>({
  command: "back",
  async execute(_args, ctx) {
    const result = await ctx.orchestra.pressKey("back");
    if (!result.ok) {
      return { ok: false, detail: result.reason };
    }
    return { ok: true, detail: "Pressed back" };
  },
});
