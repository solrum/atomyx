import { invoke, Channel } from "@tauri-apps/api/core";

import type {
  ScreenMirror,
  TouchEvent,
} from "../../../domain/features/mirror/mirror.port.js";
import type {
  ClipOptions,
  H264Frame,
  MirrorOptions,
  MirrorSession,
  MirrorTarget,
  RecordingHandle,
  Unsubscribe,
} from "../../../domain/features/mirror/mirror.types.js";
import type { MirrorTouchSink } from "../../../domain/features/mirror/mirror-touch-sink.js";
import { ClassifiedTouchSink } from "./mirror-classified-touch-sink.js";
import { SimHidTouchSink } from "./mirror-sim-hid-touch-sink.js";

const BACKEND = "simctl";

/**
 * iOS Simulator `ScreenMirror` adapter. Uses ScreenCaptureKit +
 * VideoToolbox in the Swift helper to capture and encode the simulator
 * window, then streams Annex-B NAL units to the webview over a
 * localhost WebSocket connection.
 *
 * After `mirror_simctl_start` resolves, `mirror_simctl_get_endpoint`
 * is called to learn the WS port. A WebSocket is opened immediately
 * and binary frames (1-byte tag + NAL payload) are dispatched to
 * frame listeners. The fMP4 Channel path is preserved as a fallback
 * when the helper is started with ATOMYX_MIRROR_BACKEND=fmp4.
 *
 * Contract between TS and Rust (src-tauri/src/mirror/sck.rs):
 *
 *   - `mirror_simctl_start` — args: { target, opts, frameChannel };
 *     resolves with WireSession.
 *   - `mirror_simctl_get_endpoint` — args: { sessionId }; resolves
 *     with { port, transport }.
 *   - `mirror_simctl_stop` — args: { sessionId }; resolves void.
 */
export class SimctlScreenMirror implements ScreenMirror {
  private readonly frameListeners = new Map<
    string,
    Set<(frame: H264Frame) => void>
  >();

  // Active WebSocket connections keyed by session id. Closed on stop.
  private readonly wsSockets = new Map<string, WebSocket>();

  // Sessions where the sim-hid helper is ready — use streaming sink.
  // Set during start() after a sidecar status check.
  private readonly simHidSessions = new Set<string>();

  async start(
    target: MirrorTarget,
    opts: MirrorOptions = {},
  ): Promise<MirrorSession> {
    if (target.kind !== "ios-simulator") {
      throw new Error(
        `SimctlScreenMirror: refusing target ${target.kind} (expected ios-simulator).`,
      );
    }

    // The fMP4 fallback path still needs a channel. For the WS path
    // the channel is unused but must be provided to satisfy the Rust
    // command signature — Rust branches on the handshake transport
    // before forwarding anything to it.
    const channel = new Channel<WireFrame>();
    channel.onmessage = (wire) => this.dispatchFrame(wire);

    const raw = await invoke<WireSession>("mirror_simctl_start", {
      target,
      opts,
      frameChannel: channel,
    });

    const session: MirrorSession = {
      id: raw.id,
      target,
      startedAt: raw.startedAt,
      backend: BACKEND,
      videoWidth: 0,
      videoHeight: 0,
      capabilities: {
        supportsRecording: false,
        supportsTouch: true,
        supportsKeyboard: true,
        supportsLiveTyping: true,
        supportsPinch: true,
      },
    };
    this.frameListeners.set(session.id, new Set());

    // Resolve the transport endpoint and open the WS if applicable.
    const endpoint = await invoke<WireEndpoint>(
      "mirror_simctl_get_endpoint",
      { sessionId: session.id },
    );

    if (endpoint.transport === "ws") {
      this.openWs(session.id, endpoint.port);
    }
    // For "stdout-fmp4" the channel.onmessage path above handles frames.

    // Detect whether the sim-hid helper is ready for this device.
    // When it is, createTouchSink() will return a SimHidTouchSink
    // instead of the classified fallback.
    try {
      const hidStatus = await invoke<{ state: string }>(
        "ios_sim_hid_status",
        { udid: target.id },
      );
      if (hidStatus.state === "ready") {
        this.simHidSessions.add(session.id);
      }
    } catch {
      // Sidecar may not yet have started sim-hid — use classified path.
    }

    return session;
  }

  /**
   * Open a WebSocket to the helper's local server and dispatch binary
   * frames to registered listeners. Reconnection is not attempted —
   * a WS close surfaces as an error and the session should be stopped.
   */
  private openWs(sessionId: string, port: number): void {
    const ws = new WebSocket(`ws://127.0.0.1:${port}`);
    ws.binaryType = "arraybuffer";
    this.wsSockets.set(sessionId, ws);

    ws.onmessage = (evt: MessageEvent) => {
      if (!(evt.data instanceof ArrayBuffer)) return;
      const buf = new Uint8Array(evt.data);
      if (buf.length < 2) return;
      const tag = buf[0]!;
      const nal = buf.slice(1);
      // tag 0x01 = SPS/PPS config, 0x02 = IDR keyframe, 0x03 = delta.
      if (tag !== 0x01 && tag !== 0x02 && tag !== 0x03) return;
      const keyframe = tag === 0x02;
      const timestampUs = Math.floor(performance.now() * 1_000);
      this.dispatchFrame({ sessionId, nal: Array.from(nal), timestampUs, keyframe });
    };

    ws.onerror = (evt) => {
      console.error("[mirror] WS error for session", sessionId, evt);
    };

    ws.onclose = (evt) => {
      if (!evt.wasClean) {
        console.error(
          "[mirror] WS connection lost for session",
          sessionId,
          `code=${evt.code}`,
        );
      }
      this.wsSockets.delete(sessionId);
    };
  }

  async sendTouch(session: MirrorSession, event: TouchEvent): Promise<void> {
    // SCK helper learns frame dimensions from the first decoded
    // frame; `MirrorSession.videoWidth/Height` stay 0 until that
    // happens. Callers must supply `srcWidth/srcHeight` from the
    // rendered canvas so the ratio is defined before the first
    // frame lands; rejecting is the only correct option when both
    // sources are zero.
    const w = event.srcWidth ?? session.videoWidth;
    const h = event.srcHeight ?? session.videoHeight;
    if (!w || !h) {
      throw new Error(
        "SimctlScreenMirror: cannot map touch — source dimensions unknown. Wait for the first frame or pass srcWidth/srcHeight.",
      );
    }
    const xRatio = clampUnit(event.x / w);
    const yRatio = clampUnit(event.y / h);
    await invoke("mirror_simctl_send_touch", {
      sessionId: session.id,
      deviceId: session.target.id,
      action: event.action,
      xRatio,
      yRatio,
      bundleId: null,
    });
  }

  async longPressAt(
    session: MirrorSession,
    point: { readonly x: number; readonly y: number; readonly srcWidth?: number; readonly srcHeight?: number },
    durationMs: number,
  ): Promise<void> {
    const { w, h } = this.requireSourceDims(session, point);
    await invoke("mirror_simctl_long_press", {
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
    const { w, h } = this.requireSourceDims(session, from);
    await invoke("mirror_simctl_swipe", {
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

  async pinch(
    session: MirrorSession,
    center: { readonly xRatio: number; readonly yRatio: number },
    fromScale: number,
    toScale: number,
    durationMs: number,
  ): Promise<void> {
    await invoke("mirror_pinch", {
      sessionId: session.id,
      deviceId: session.target.id,
      centerXRatio: clampUnit(center.xRatio),
      centerYRatio: clampUnit(center.yRatio),
      fromScale,
      toScale,
      durationMs,
      bundleId: null,
    });
  }

  async inputText(session: MirrorSession, text: string): Promise<void> {
    await invoke("mirror_input_text", {
      sessionId: session.id,
      deviceId: session.target.id,
      text,
    });
  }

  async eraseText(session: MirrorSession, count: number): Promise<void> {
    await invoke("mirror_erase_text", {
      sessionId: session.id,
      deviceId: session.target.id,
      count,
    });
  }

  async pressKey(session: MirrorSession, key: string): Promise<void> {
    await invoke("mirror_press_key", {
      sessionId: session.id,
      deviceId: session.target.id,
      key,
    });
  }

  private requireSourceDims(
    session: MirrorSession,
    hint: { readonly srcWidth?: number; readonly srcHeight?: number },
  ): { readonly w: number; readonly h: number } {
    const w = hint.srcWidth ?? session.videoWidth;
    const h = hint.srcHeight ?? session.videoHeight;
    if (!w || !h) {
      throw new Error(
        "SimctlScreenMirror: cannot map gesture — source dimensions unknown. Wait for the first frame or pass srcWidth/srcHeight.",
      );
    }
    return { w, h };
  }

  createTouchSink(session: MirrorSession): MirrorTouchSink {
    if (this.simHidSessions.has(session.id)) {
      // Streaming path: down/move/up forwarded directly to the
      // sim-hid helper. Requires arm64 + Xcode 26+ on the host.
      return new SimHidTouchSink(
        session,
        session.target.id,
        session.videoWidth,
        session.videoHeight,
      );
    }
    return new ClassifiedTouchSink(this, session);
  }

  async stop(session: MirrorSession): Promise<void> {
    // Close the WS before stopping the helper process so the helper
    // sees a clean client disconnect rather than a pipe-break error.
    const ws = this.wsSockets.get(session.id);
    if (ws) {
      ws.close();
      this.wsSockets.delete(session.id);
    }
    await invoke("mirror_simctl_stop", { sessionId: session.id });
    this.frameListeners.delete(session.id);
    this.simHidSessions.delete(session.id);
  }

  onFrame(
    session: MirrorSession,
    listener: (frame: H264Frame) => void,
  ): Unsubscribe {
    const set = this.frameListeners.get(session.id);
    if (!set) {
      throw new Error(
        `SimctlScreenMirror: unknown session ${session.id} — start() first.`,
      );
    }
    set.add(listener);
    return () => set.delete(listener);
  }

  async record(
    session: MirrorSession,
    outputPath: string,
  ): Promise<RecordingHandle> {
    await invoke("mirror_simctl_record", { sessionId: session.id, outputPath });
    return { session, outputPath };
  }

  async stopRecording(handle: RecordingHandle): Promise<void> {
    await invoke("mirror_simctl_stop_recording", {
      sessionId: handle.session.id,
    });
  }

  async clipFromRecording(
    handle: RecordingHandle,
    opts: ClipOptions,
  ): Promise<string> {
    return invoke<string>("mirror_simctl_clip", {
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
}

interface WireEndpoint {
  readonly port: number;
  readonly transport: string;
}

interface WireFrame {
  readonly sessionId: string;
  readonly nal: readonly number[];
  readonly timestampUs: number;
  readonly keyframe: boolean;
}

function clampUnit(v: number): number {
  if (!Number.isFinite(v)) return 0;
  if (v < 0) return 0;
  if (v > 1) return 1;
  return v;
}
