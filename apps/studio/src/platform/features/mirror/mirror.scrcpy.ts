import { invoke, Channel } from "@tauri-apps/api/core";

import type {
  ScreenMirror,
  TouchEvent,
} from "../../../domain/features/mirror/mirror.port.js";
import type {
  ClipOptions,
  H264Frame,
  MirrorCapabilities,
  MirrorOptions,
  MirrorSession,
  MirrorTarget,
  RecordingHandle,
  Unsubscribe,
} from "../../../domain/features/mirror/mirror.types.js";
import type { MirrorTouchSink } from "../../../domain/features/mirror/mirror-touch-sink.js";
import { StreamingTouchSink } from "./mirror-streaming-touch-sink.js";

const BACKEND = "scrcpy";

/**
 * Android `ScreenMirror` adapter. All subprocess work (adb push of
 * scrcpy-server.jar, reverse tunnel, NAL parsing, MP4 muxing) lives
 * in the Tauri Rust side; this class owns only the typed command
 * surface and the per-session frame channel.
 *
 * Contract between TS and Rust (must stay in sync with
 * `src-tauri/src/mirror/scrcpy.rs`):
 *
 *   - `mirror_scrcpy_start` — args: { target, opts, frameChannel };
 *     resolves with MirrorSession.
 *   - `mirror_scrcpy_stop` — args: { sessionId }; resolves void.
 *   - `mirror_scrcpy_record` — args: { sessionId, outputPath };
 *     resolves with RecordingHandle.
 *   - `mirror_scrcpy_clip` — args: { sessionId, outputPath,
 *     startMs, endMs }; resolves with absolute path of clip.
 *
 * Target guard: callers outside the dispatcher may pass any target
 * kind; this adapter rejects non-Android targets at `start` so a
 * mis-wired composition root fails loud rather than silently
 * activating the wrong backend.
 */
export class ScrcpyScreenMirror implements ScreenMirror {
  private readonly frameListeners = new Map<
    string,
    Set<(frame: H264Frame) => void>
  >();
  private readonly invokeFn: typeof invoke;

  // `invoke` is injected so the command surface can be unit-tested
  // without a Tauri runtime; production wiring uses the default.
  constructor(invokeFn: typeof invoke = invoke) {
    this.invokeFn = invokeFn;
  }

  async start(
    target: MirrorTarget,
    opts: MirrorOptions = {},
  ): Promise<MirrorSession> {
    if (target.kind !== "android") {
      throw new Error(
        `ScrcpyScreenMirror: refusing target ${target.kind} (expected android).`,
      );
    }

    const channel = new Channel<WireFrame>();
    channel.onmessage = (wire) => this.dispatchFrame(wire);

    const raw = await this.invokeFn<WireSession>("mirror_scrcpy_start", {
      target,
      opts,
      frameChannel: channel,
    });

    const session: MirrorSession = {
      id: raw.id,
      target,
      startedAt: raw.startedAt,
      backend: BACKEND,
      videoWidth: raw.videoWidth,
      videoHeight: raw.videoHeight,
      capabilities: scrcpyCapabilities(),
    };
    this.frameListeners.set(session.id, new Set());
    return session;
  }

  async sendTouch(session: MirrorSession, event: TouchEvent): Promise<void> {
    await this.invokeFn("mirror_scrcpy_send_touch", {
      sessionId: session.id,
      action: event.action,
      x: event.x,
      y: event.y,
      pressure: event.pressure,
    });
  }

  // Gesture and keyboard input route through the same device-neutral
  // sidecar commands the iOS adapter uses — the dispatch resolves the
  // bound device by id, independent of the mirror frame channel. The
  // `mirror_simctl_*` command names are device-neutral despite the
  // prefix.
  async longPressAt(
    session: MirrorSession,
    point: { readonly x: number; readonly y: number; readonly srcWidth?: number; readonly srcHeight?: number },
    durationMs: number,
  ): Promise<void> {
    const { w, h } = requireSourceDims(session, point);
    await this.invokeFn("mirror_simctl_long_press", {
      sessionId: session.id,
      deviceId: session.target.id,
      xRatio: clampUnit(point.x / w),
      yRatio: clampUnit(point.y / h),
      durationMs,
      bundleId: null,
    });
  }

  async swipe(
    session: MirrorSession,
    from: { readonly x: number; readonly y: number; readonly srcWidth?: number; readonly srcHeight?: number },
    to: { readonly x: number; readonly y: number },
    durationMs: number,
  ): Promise<void> {
    const { w, h } = requireSourceDims(session, from);
    await this.invokeFn("mirror_simctl_swipe", {
      sessionId: session.id,
      deviceId: session.target.id,
      fromXRatio: clampUnit(from.x / w),
      fromYRatio: clampUnit(from.y / h),
      toXRatio: clampUnit(to.x / w),
      toYRatio: clampUnit(to.y / h),
      durationMs,
      bundleId: null,
    });
  }

  async pinch(): Promise<void> {
    // Guarded by capabilities.supportsPinch === false; the UI never
    // calls this. Kept as a loud failure for a mis-wired caller.
    throw new Error(
      "ScrcpyScreenMirror.pinch: Android has no simultaneous two-finger gesture — gate on capabilities.supportsPinch.",
    );
  }

  async inputText(session: MirrorSession, text: string): Promise<void> {
    await this.invokeFn("mirror_input_text", {
      sessionId: session.id,
      deviceId: session.target.id,
      text,
    });
  }

  async eraseText(session: MirrorSession, count: number): Promise<void> {
    await this.invokeFn("mirror_erase_text", {
      sessionId: session.id,
      deviceId: session.target.id,
      count,
    });
  }

  async pressKey(session: MirrorSession, key: string): Promise<void> {
    await this.invokeFn("mirror_press_key", {
      sessionId: session.id,
      deviceId: session.target.id,
      key,
    });
  }

  createTouchSink(session: MirrorSession): MirrorTouchSink {
    return new StreamingTouchSink(this, session);
  }

  async stop(session: MirrorSession): Promise<void> {
    await this.invokeFn("mirror_scrcpy_stop", { sessionId: session.id });
    this.frameListeners.delete(session.id);
  }

  onFrame(
    session: MirrorSession,
    listener: (frame: H264Frame) => void,
  ): Unsubscribe {
    const set = this.frameListeners.get(session.id);
    if (!set) {
      throw new Error(
        `ScrcpyScreenMirror: unknown session ${session.id} — start() first.`,
      );
    }
    set.add(listener);
    return () => set.delete(listener);
  }

  async record(
    session: MirrorSession,
    outputPath: string,
  ): Promise<RecordingHandle> {
    await this.invokeFn("mirror_scrcpy_record", { sessionId: session.id, outputPath });
    return { session, outputPath };
  }

  async stopRecording(handle: RecordingHandle): Promise<void> {
    await this.invokeFn("mirror_scrcpy_stop_recording", {
      sessionId: handle.session.id,
    });
  }

  async clipFromRecording(
    handle: RecordingHandle,
    opts: ClipOptions,
  ): Promise<string> {
    return this.invokeFn<string>("mirror_scrcpy_clip", {
      sessionId: handle.session.id,
      outputPath: opts.outputPath,
      startMs: opts.startMs,
      endMs: opts.endMs,
    });
  }

  private dispatchFrame(wire: WireFrame): void {
    const set = this.frameListeners.get(wire.sessionId);
    if (!set || set.size === 0) return;
    const frame: H264Frame = {
      nal: Uint8Array.from(wire.nal),
      timestampUs: wire.timestampUs,
      keyframe: wire.keyframe,
    };
    for (const listener of set) {
      listener(frame);
    }
  }
}

interface WireSession {
  readonly id: string;
  readonly startedAt: number;
  readonly videoWidth: number;
  readonly videoHeight: number;
}

interface WireFrame {
  readonly sessionId: string;
  readonly nal: readonly number[];
  readonly timestampUs: number;
  readonly keyframe: boolean;
}

function requireSourceDims(
  session: MirrorSession,
  hint: { readonly srcWidth?: number; readonly srcHeight?: number },
): { readonly w: number; readonly h: number } {
  const w = hint.srcWidth ?? session.videoWidth;
  const h = hint.srcHeight ?? session.videoHeight;
  if (!w || !h) {
    throw new Error(
      "ScrcpyScreenMirror: cannot map gesture — source dimensions unknown.",
    );
  }
  return { w, h };
}

function clampUnit(v: number): number {
  if (!Number.isFinite(v)) return 0;
  if (v < 0) return 0;
  if (v > 1) return 1;
  return v;
}

export function scrcpyCapabilities(): MirrorCapabilities {
  return {
    supportsRecording: false,
    supportsTouch: true,
    supportsKeyboard: true,
    // Android's text primitive clears the whole focused field rather
    // than deleting per character, so live keystroke streaming would
    // wipe the buffer on the first correction — only the
    // commit-on-Enter insert path is offered.
    supportsLiveTyping: false,
    // The accessibility service ships single-stroke gestures only; a
    // simultaneous two-finger pinch is not available.
    supportsPinch: false,
  };
}
