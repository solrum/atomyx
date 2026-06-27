import type { Dispatcher } from "../../infra/transport/dispatcher.js";
import type { InputService } from "./input.service.js";

export function registerInputHandlers(
  dispatcher: Dispatcher,
  service: InputService,
): void {
  dispatcher.register("tapRatio", async (params) => {
    const p = (params ?? {}) as {
      xRatio?: unknown;
      yRatio?: unknown;
      bundleId?: unknown;
    };
    const xRatio = typeof p.xRatio === "number" ? p.xRatio : NaN;
    const yRatio = typeof p.yRatio === "number" ? p.yRatio : NaN;
    if (!Number.isFinite(xRatio) || !Number.isFinite(yRatio)) {
      throw new Error("tapRatio requires numeric xRatio and yRatio");
    }
    const bundleId =
      typeof p.bundleId === "string" && p.bundleId.length > 0
        ? p.bundleId
        : undefined;
    process.stderr.write(
      `[input] tapRatio xRatio=${xRatio.toFixed(4)} yRatio=${yRatio.toFixed(4)} bundleId=${bundleId ?? "<none>"}\n`,
    );
    try {
      await service.tapRatio({ xRatio, yRatio, bundleId });
      process.stderr.write(`[input] tapRatio dispatched\n`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      process.stderr.write(`[input] tapRatio dispatch failed: ${msg}\n`);
      throw err;
    }
    return null;
  });
}
