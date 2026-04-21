import { defineCommand } from "@atomyx/driver/script";
import type { CaptureStep } from "@atomyx/shared/script";

export const captureCommand = defineCommand<CaptureStep>({
  command: "capture",
  async execute(args, ctx) {
    if (!ctx.networkCapture) {
      return {
        ok: false,
        detail:
          "No network capture adapter configured. Pass --proxy to the CLI " +
          "or set captureConfig in runner options.",
      };
    }

    const timeoutMs = 10_000;
    try {
      const captured = await ctx.networkCapture.waitForRequest(
        args.pattern,
        timeoutMs,
      );
      ctx.captures.set(args.as, captured);
      return {
        ok: true,
        detail: `Captured ${captured.method} ${captured.url} → ${captured.status} as "${args.as}"`,
      };
    } catch (err) {
      return {
        ok: false,
        detail: (err as Error).message,
      };
    }
  },
});
