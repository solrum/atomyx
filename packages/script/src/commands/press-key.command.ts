import { defineCommand } from "@atomyx/driver/script";
import type { PressKeyStep } from "@atomyx/shared/script";

export const pressKeyCommand = defineCommand<PressKeyStep>({
  command: "pressKey",
  async execute(args, ctx) {
    const result = await ctx.orchestra.pressKey(args.key);
    if (!result.ok) {
      return { ok: false, detail: result.reason };
    }
    return { ok: true, detail: `Pressed ${args.key}` };
  },
});
