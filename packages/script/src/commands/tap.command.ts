import { defineCommand } from "@atomyx/driver/script";
import type { TapStep } from "@atomyx/shared/script";
import { compileScriptSelector } from "../parser/selector-compiler.js";

export const tapCommand = defineCommand<TapStep>({
  command: "tap",
  async execute(args, ctx) {
    const selector = compileScriptSelector(args.selector);
    const result = await ctx.orchestra.tap(selector);
    if (!result.ok) {
      return { ok: false, detail: result.reason };
    }
    return { ok: true, detail: result.detail };
  },
});
