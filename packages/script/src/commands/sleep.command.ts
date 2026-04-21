import { defineCommand } from "@atomyx/driver/script";
import type { SleepStep } from "@atomyx/shared/script";

export const sleepCommand = defineCommand<SleepStep>({
  command: "sleep",
  async execute(args, ctx) {
    await ctx.clock.sleep(args.ms);
    return { ok: true, detail: `Slept ${args.ms}ms` };
  },
});
