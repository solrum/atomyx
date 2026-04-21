import { defineCommand } from "@atomyx/driver/script";
import type { SwipeStep } from "@atomyx/shared/script";

export const swipeCommand = defineCommand<SwipeStep>({
  command: "swipe",
  async execute(args, ctx) {
    await ctx.orchestra.swipeDirection(args.direction);
    return { ok: true, detail: `Swiped ${args.direction}` };
  },
});
