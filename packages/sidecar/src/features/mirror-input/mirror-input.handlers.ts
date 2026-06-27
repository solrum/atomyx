import type { Dispatcher } from "../../infra/transport/dispatcher.js";
import type { MirrorInputService } from "./mirror-input.service.js";
import { DriverNotReadyError } from "./mirror-input.errors.js";

export function registerMirrorInputHandlers(
  dispatcher: Dispatcher,
  service: MirrorInputService,
): void {
  dispatcher.register("mirrorTapRatio", async (params) => {
    const p = (params ?? {}) as {
      deviceId?: unknown;
      xRatio?: unknown;
      yRatio?: unknown;
      bundleId?: unknown;
    };
    const deviceId = typeof p.deviceId === "string" ? p.deviceId : "";
    const xRatio = typeof p.xRatio === "number" ? p.xRatio : NaN;
    const yRatio = typeof p.yRatio === "number" ? p.yRatio : NaN;
    if (!deviceId) {
      throw new Error("mirrorTapRatio requires a non-empty deviceId");
    }
    if (!Number.isFinite(xRatio) || !Number.isFinite(yRatio)) {
      throw new Error("mirrorTapRatio requires numeric xRatio and yRatio");
    }
    const bundleId =
      typeof p.bundleId === "string" && p.bundleId.length > 0
        ? p.bundleId
        : undefined;
    process.stderr.write(
      `[mirror-input] tapRatio device=${deviceId} xRatio=${xRatio.toFixed(4)} yRatio=${yRatio.toFixed(4)} bundleId=${bundleId ?? "<none>"}\n`,
    );
    try {
      await service.tapRatio({ deviceId, xRatio, yRatio, bundleId });
      process.stderr.write(`[mirror-input] tapRatio dispatched\n`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      process.stderr.write(`[mirror-input] tapRatio failed: ${msg}\n`);
      throw err;
    }
    return null;
  });

  dispatcher.register("mirrorLongPressRatio", async (params) => {
    const p = (params ?? {}) as {
      deviceId?: unknown;
      xRatio?: unknown;
      yRatio?: unknown;
      durationMs?: unknown;
      bundleId?: unknown;
    };
    const deviceId = typeof p.deviceId === "string" ? p.deviceId : "";
    const xRatio = typeof p.xRatio === "number" ? p.xRatio : NaN;
    const yRatio = typeof p.yRatio === "number" ? p.yRatio : NaN;
    if (!deviceId) {
      throw new Error("mirrorLongPressRatio requires a non-empty deviceId");
    }
    if (!Number.isFinite(xRatio) || !Number.isFinite(yRatio)) {
      throw new Error(
        "mirrorLongPressRatio requires numeric xRatio and yRatio",
      );
    }
    const durationMs =
      typeof p.durationMs === "number" && Number.isFinite(p.durationMs)
        ? Math.max(0, Math.round(p.durationMs))
        : undefined;
    const bundleId =
      typeof p.bundleId === "string" && p.bundleId.length > 0
        ? p.bundleId
        : undefined;
    process.stderr.write(
      `[mirror-input] longPressRatio device=${deviceId} xRatio=${xRatio.toFixed(4)} yRatio=${yRatio.toFixed(4)} durationMs=${durationMs ?? "default"} bundleId=${bundleId ?? "<none>"}\n`,
    );
    try {
      await service.longPressRatio({
        deviceId,
        xRatio,
        yRatio,
        durationMs,
        bundleId,
      });
      process.stderr.write(`[mirror-input] longPressRatio dispatched\n`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      process.stderr.write(`[mirror-input] longPressRatio failed: ${msg}\n`);
      throw err;
    }
    return null;
  });

  dispatcher.register("mirrorSwipeRatio", async (params) => {
    const p = (params ?? {}) as {
      deviceId?: unknown;
      fromXRatio?: unknown;
      fromYRatio?: unknown;
      toXRatio?: unknown;
      toYRatio?: unknown;
      durationMs?: unknown;
      bundleId?: unknown;
    };
    const deviceId = typeof p.deviceId === "string" ? p.deviceId : "";
    const fromXRatio =
      typeof p.fromXRatio === "number" ? p.fromXRatio : NaN;
    const fromYRatio =
      typeof p.fromYRatio === "number" ? p.fromYRatio : NaN;
    const toXRatio = typeof p.toXRatio === "number" ? p.toXRatio : NaN;
    const toYRatio = typeof p.toYRatio === "number" ? p.toYRatio : NaN;
    if (!deviceId) {
      throw new Error("mirrorSwipeRatio requires a non-empty deviceId");
    }
    if (
      !Number.isFinite(fromXRatio) ||
      !Number.isFinite(fromYRatio) ||
      !Number.isFinite(toXRatio) ||
      !Number.isFinite(toYRatio)
    ) {
      throw new Error(
        "mirrorSwipeRatio requires numeric fromXRatio/fromYRatio/toXRatio/toYRatio",
      );
    }
    const durationMs =
      typeof p.durationMs === "number" && Number.isFinite(p.durationMs)
        ? Math.max(0, Math.round(p.durationMs))
        : undefined;
    const bundleId =
      typeof p.bundleId === "string" && p.bundleId.length > 0
        ? p.bundleId
        : undefined;
    process.stderr.write(
      `[mirror-input] swipeRatio device=${deviceId} from=${fromXRatio.toFixed(4)},${fromYRatio.toFixed(4)} to=${toXRatio.toFixed(4)},${toYRatio.toFixed(4)} durationMs=${durationMs ?? "default"} bundleId=${bundleId ?? "<none>"}\n`,
    );
    try {
      await service.swipeRatio({
        deviceId,
        fromXRatio,
        fromYRatio,
        toXRatio,
        toYRatio,
        durationMs,
        bundleId,
      });
      process.stderr.write(`[mirror-input] swipeRatio dispatched\n`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      process.stderr.write(`[mirror-input] swipeRatio failed: ${msg}\n`);
      throw err;
    }
    return null;
  });

  // ── Streaming touch handlers ─────────────────────────────────

  function parseStreamingParams(params: unknown): {
    deviceId: string;
    xRatio: number;
    yRatio: number;
    touchId: number;
  } {
    const p = (params ?? {}) as {
      deviceId?: unknown;
      xRatio?: unknown;
      yRatio?: unknown;
      touchId?: unknown;
    };
    const deviceId = typeof p.deviceId === "string" ? p.deviceId : "";
    const xRatio = typeof p.xRatio === "number" ? p.xRatio : NaN;
    const yRatio = typeof p.yRatio === "number" ? p.yRatio : NaN;
    const touchId =
      typeof p.touchId === "number" && Number.isFinite(p.touchId)
        ? Math.round(p.touchId)
        : 1;
    if (!deviceId) {
      throw new Error("streaming touch requires a non-empty deviceId");
    }
    if (!Number.isFinite(xRatio) || !Number.isFinite(yRatio)) {
      throw new Error("streaming touch requires numeric xRatio and yRatio");
    }
    return { deviceId, xRatio, yRatio, touchId };
  }

  dispatcher.register("mirrorStreamingTouchDown", async (params) => {
    const p = parseStreamingParams(params);
    process.stderr.write(
      `[mirror-input] streamingDown device=${p.deviceId} x=${p.xRatio.toFixed(4)} y=${p.yRatio.toFixed(4)} id=${p.touchId}\n`,
    );
    try {
      await service.streamingDown(p);
    } catch (err) {
      if (!(err instanceof DriverNotReadyError)) {
        const msg = err instanceof Error ? err.message : String(err);
        process.stderr.write(`[mirror-input] streamingDown failed: ${msg}\n`);
      }
      throw err;
    }
    return null;
  });

  dispatcher.register("mirrorStreamingTouchMove", async (params) => {
    const p = parseStreamingParams(params);
    try {
      await service.streamingMove(p);
    } catch (err) {
      if (!(err instanceof DriverNotReadyError)) {
        const msg = err instanceof Error ? err.message : String(err);
        process.stderr.write(`[mirror-input] streamingMove failed: ${msg}\n`);
      }
      throw err;
    }
    return null;
  });

  dispatcher.register("mirrorStreamingTouchUp", async (params) => {
    const p = parseStreamingParams(params);
    process.stderr.write(
      `[mirror-input] streamingUp device=${p.deviceId} x=${p.xRatio.toFixed(4)} y=${p.yRatio.toFixed(4)} id=${p.touchId}\n`,
    );
    try {
      await service.streamingUp(p);
    } catch (err) {
      if (!(err instanceof DriverNotReadyError)) {
        const msg = err instanceof Error ? err.message : String(err);
        process.stderr.write(`[mirror-input] streamingUp failed: ${msg}\n`);
      }
      throw err;
    }
    return null;
  });

  dispatcher.register("mirrorInputText", async (params) => {
    const p = (params ?? {}) as { deviceId?: unknown; text?: unknown };
    const deviceId = typeof p.deviceId === "string" ? p.deviceId : "";
    const text = typeof p.text === "string" ? p.text : null;
    if (!deviceId) {
      throw new Error("mirrorInputText requires a non-empty deviceId");
    }
    if (text === null) {
      throw new Error("mirrorInputText requires a string text");
    }
    process.stderr.write(
      `[mirror-input] inputText device=${deviceId} len=${text.length}\n`,
    );
    try {
      await service.inputText({ deviceId, text });
      process.stderr.write(`[mirror-input] inputText dispatched\n`);
    } catch (err) {
      if (!(err instanceof DriverNotReadyError)) {
        const msg = err instanceof Error ? err.message : String(err);
        process.stderr.write(`[mirror-input] inputText failed: ${msg}\n`);
      }
      throw err;
    }
    return null;
  });

  dispatcher.register("mirrorEraseText", async (params) => {
    const p = (params ?? {}) as { deviceId?: unknown; count?: unknown };
    const deviceId = typeof p.deviceId === "string" ? p.deviceId : "";
    const count = typeof p.count === "number" ? p.count : NaN;
    if (!deviceId) {
      throw new Error("mirrorEraseText requires a non-empty deviceId");
    }
    if (!Number.isFinite(count) || count <= 0) {
      throw new Error("mirrorEraseText requires a positive numeric count");
    }
    try {
      await service.eraseText({ deviceId, count });
    } catch (err) {
      if (!(err instanceof DriverNotReadyError)) {
        const msg = err instanceof Error ? err.message : String(err);
        process.stderr.write(`[mirror-input] eraseText failed: ${msg}\n`);
      }
      throw err;
    }
    return null;
  });

  dispatcher.register("mirrorPressKey", async (params) => {
    const p = (params ?? {}) as { deviceId?: unknown; key?: unknown };
    const deviceId = typeof p.deviceId === "string" ? p.deviceId : "";
    const key = typeof p.key === "string" && p.key.length > 0 ? p.key : "";
    if (!deviceId) {
      throw new Error("mirrorPressKey requires a non-empty deviceId");
    }
    if (!key) {
      throw new Error("mirrorPressKey requires a non-empty key");
    }
    process.stderr.write(`[mirror-input] pressKey device=${deviceId} key=${key}\n`);
    try {
      await service.pressKey({ deviceId, key });
    } catch (err) {
      if (!(err instanceof DriverNotReadyError)) {
        const msg = err instanceof Error ? err.message : String(err);
        process.stderr.write(`[mirror-input] pressKey failed: ${msg}\n`);
      }
      throw err;
    }
    return null;
  });

  dispatcher.register("mirrorPinch", async (params) => {
    const p = (params ?? {}) as {
      deviceId?: unknown;
      centerXRatio?: unknown;
      centerYRatio?: unknown;
      fromScale?: unknown;
      toScale?: unknown;
      durationMs?: unknown;
      bundleId?: unknown;
    };
    const deviceId = typeof p.deviceId === "string" ? p.deviceId : "";
    const centerXRatio = typeof p.centerXRatio === "number" ? p.centerXRatio : NaN;
    const centerYRatio = typeof p.centerYRatio === "number" ? p.centerYRatio : NaN;
    const fromScale = typeof p.fromScale === "number" ? p.fromScale : NaN;
    const toScale = typeof p.toScale === "number" ? p.toScale : NaN;
    if (!deviceId) {
      throw new Error("mirrorPinch requires a non-empty deviceId");
    }
    if (
      !Number.isFinite(centerXRatio) ||
      !Number.isFinite(centerYRatio) ||
      !Number.isFinite(fromScale) ||
      !Number.isFinite(toScale)
    ) {
      throw new Error(
        "mirrorPinch requires numeric centerXRatio, centerYRatio, fromScale, toScale",
      );
    }
    const durationMs = typeof p.durationMs === "number" ? p.durationMs : undefined;
    const bundleId =
      typeof p.bundleId === "string" && p.bundleId.length > 0
        ? p.bundleId
        : undefined;
    process.stderr.write(
      `[mirror-input] pinch device=${deviceId} center=${centerXRatio.toFixed(4)},${centerYRatio.toFixed(4)} scale=${fromScale.toFixed(2)}→${toScale.toFixed(2)}\n`,
    );
    try {
      await service.pinchRatio({
        deviceId,
        centerXRatio,
        centerYRatio,
        fromScale,
        toScale,
        durationMs,
        bundleId,
      });
      process.stderr.write(`[mirror-input] pinch dispatched\n`);
    } catch (err) {
      if (!(err instanceof DriverNotReadyError)) {
        const msg = err instanceof Error ? err.message : String(err);
        process.stderr.write(`[mirror-input] pinch failed: ${msg}\n`);
      }
      throw err;
    }
    return null;
  });
}
