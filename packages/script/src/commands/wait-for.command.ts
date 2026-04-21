import { defineCommand, type CommandResult } from "@atomyx/driver/script";
import { FindTimeoutError } from "@atomyx/driver/finder";
import type { WaitForStep } from "@atomyx/shared/script";
import { compileScriptSelector } from "../parser/selector-compiler.js";

export const waitForCommand = defineCommand<WaitForStep>({
  command: "waitFor",
  async execute(args, ctx): Promise<CommandResult> {
    const selector = compileScriptSelector(args.selector);
    const timeoutMs = args.timeoutMs ?? 5000;
    try {
      await ctx.orchestra.waitFor(selector, { timeoutMs });
      return { ok: true, detail: "Element appeared" };
    } catch (err) {
      if (err instanceof FindTimeoutError) {
        return {
          ok: false,
          detail: `Element not found within ${timeoutMs}ms`,
        };
      }
      throw err;
    }
  },
});
