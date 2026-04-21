import { defineCommand, type CommandResult } from "@atomyx/driver/script";
import { FindTimeoutError } from "@atomyx/driver/finder";
import type { AssertVisibleStep } from "@atomyx/shared/script";
import { compileScriptSelector } from "../parser/selector-compiler.js";

export const assertVisibleCommand = defineCommand<AssertVisibleStep>({
  command: "assertVisible",
  async execute(args, ctx): Promise<CommandResult> {
    const selector = compileScriptSelector(args.selector);

    if (args.timeoutMs) {
      // Poll until element appears or timeout
      try {
        await ctx.orchestra.waitFor(selector, { timeoutMs: args.timeoutMs });
        return { ok: true, detail: "Element is visible" };
      } catch (err) {
        if (err instanceof FindTimeoutError) {
          return {
            ok: false,
            detail: `Element not visible within ${args.timeoutMs}ms`,
          };
        }
        throw err;
      }
    }

    // Instant check (no polling)
    const cursor = await ctx.orchestra.findOne(selector);
    if (!cursor) {
      return { ok: false, detail: "Element not visible on screen" };
    }
    return { ok: true, detail: "Element is visible" };
  },
});
