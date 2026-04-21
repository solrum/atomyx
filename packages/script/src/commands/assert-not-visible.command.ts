import { defineCommand, type CommandResult } from "@atomyx/driver/script";
import type { AssertNotVisibleStep } from "@atomyx/shared/script";
import { compileScriptSelector } from "../parser/selector-compiler.js";

/** Poll interval for the disappear-wait loop. */
const DISAPPEAR_POLL_INTERVAL_MS = 100;

export const assertNotVisibleCommand = defineCommand<AssertNotVisibleStep>({
  command: "assertNotVisible",
  async execute(args, ctx): Promise<CommandResult> {
    const selector = compileScriptSelector(args.selector);

    if (args.timeoutMs) {
      // Poll until the element is gone. Inverse of `assertVisible`'s
      // waitFor — there isn't a symmetric primitive for "element
      // disappears", so we loop here. The interval is the shared
      // script-engine polling cadence (100 ms) used by `handle` and
      // the wait primitives.
      const deadline = ctx.clock.now() + args.timeoutMs;
      while (ctx.clock.now() < deadline) {
        const cursor = await ctx.orchestra.findOne(selector);
        if (!cursor) {
          return { ok: true, detail: "Element is not visible" };
        }
        await ctx.clock.sleep(DISAPPEAR_POLL_INTERVAL_MS);
      }
      return {
        ok: false,
        detail: `Element still visible after ${args.timeoutMs}ms`,
      };
    }

    // Instant check
    const cursor = await ctx.orchestra.findOne(selector);
    if (cursor) {
      return { ok: false, detail: "Element is still visible (expected not visible)" };
    }
    return { ok: true, detail: "Element is not visible" };
  },
});
