import { invoke } from "@tauri-apps/api/core";
import type {
  MirrorSession,
  MirrorTouchSink,
  TouchPoint,
} from "../../../domain/features/mirror/index.js";

const TOUCH_ID = 1;

/**
 * Touch sink for iOS Simulator sessions backed by the sim-hid path
 * (arm64 + Xcode 26+). Each pointer phase is forwarded immediately
 * via `mirror_simctl_streaming_touch` — the helper dispatches it
 * directly to the SimDeviceLegacyHIDClient without waiting for
 * gesture classification.
 *
 * The sidecar gates every call on driver readiness and rejects with
 * a clear error when the underlying driver does not implement
 * StreamingTouchCapable; that error surfaces here and is rethrown
 * so the canvas can fall back to the classified path.
 */
export class SimHidTouchSink implements MirrorTouchSink {
  constructor(
    private readonly session: MirrorSession,
    private readonly deviceId: string,
    private readonly srcWidth: number,
    private readonly srcHeight: number,
  ) {}

  async beginPress(point: TouchPoint): Promise<void> {
    await invoke("mirror_simctl_streaming_touch", {
      sessionId: this.session.id,
      deviceId: this.deviceId,
      phase: "down",
      xRatio: clamp(point.x / (point.srcWidth ?? this.srcWidth)),
      yRatio: clamp(point.y / (point.srcHeight ?? this.srcHeight)),
      touchId: TOUCH_ID,
    });
  }

  async trackTo(point: TouchPoint): Promise<void> {
    await invoke("mirror_simctl_streaming_touch", {
      sessionId: this.session.id,
      deviceId: this.deviceId,
      phase: "move",
      xRatio: clamp(point.x / (point.srcWidth ?? this.srcWidth)),
      yRatio: clamp(point.y / (point.srcHeight ?? this.srcHeight)),
      touchId: TOUCH_ID,
    });
  }

  async endPress(point: TouchPoint): Promise<void> {
    await invoke("mirror_simctl_streaming_touch", {
      sessionId: this.session.id,
      deviceId: this.deviceId,
      phase: "up",
      xRatio: clamp(point.x / (point.srcWidth ?? this.srcWidth)),
      yRatio: clamp(point.y / (point.srcHeight ?? this.srcHeight)),
      touchId: TOUCH_ID,
    });
  }
}

function clamp(v: number): number {
  if (!Number.isFinite(v)) return 0;
  return Math.max(0, Math.min(1, v));
}
