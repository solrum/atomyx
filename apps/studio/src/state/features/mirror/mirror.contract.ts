import type {
  H264Frame,
  MirrorCapabilities,
  MirrorOptions,
  MirrorTarget,
  TouchAction,
} from "../../../domain/features/mirror/types.js";
import type { MirrorTouchSink } from "../../../domain/features/mirror/touch-sink.js";

export type { MirrorTouchSink };

export interface MirrorSessionStatus {
  readonly id: string;
  readonly target: MirrorTarget;
  readonly startedAt: number;
  readonly backend: string;
  readonly isRecording: boolean;
  readonly recordingPath: string | null;
  readonly videoWidth: number;
  readonly videoHeight: number;
  readonly capabilities: MirrorCapabilities;
}

export interface MirrorTouchEvent {
  readonly action: TouchAction;
  readonly x: number;
  readonly y: number;
  readonly pressure: number;
  readonly srcWidth?: number;
  readonly srcHeight?: number;
}

export interface MirrorSnapshot {
  readonly sessions: Readonly<Record<string, MirrorSessionStatus>>;
}

export interface ClipRequest {
  readonly startMs: number;
  readonly endMs: number;
  readonly outputPath: string;
}

/**
 * Studio's user-facing mirror contract. Wraps the domain
 * `ScreenMirror` port into a React-friendly state surface.
 *
 * The live preview path decodes `onFrame` NAL units directly via
 * WebCodecs `VideoDecoder`; `startRecording` asks the adapter to
 * persist an MP4 to disk independently of the preview, and
 * `extractClip` trims that recording after the fact.
 */
export interface MirrorApi {
  getSnapshot(): MirrorSnapshot;
  subscribe(listener: () => void): () => void;

  startForTarget(target: MirrorTarget, opts?: MirrorOptions): Promise<string>;
  stop(sessionId: string): Promise<void>;

  onFrame(
    sessionId: string,
    listener: (frame: H264Frame) => void,
  ): () => void;

  startRecording(sessionId: string, outputPath: string): Promise<void>;
  stopRecording(sessionId: string): Promise<void>;
  extractClip(sessionId: string, opts: ClipRequest): Promise<string>;

  sendTouch(sessionId: string, event: MirrorTouchEvent): Promise<void>;

  /// Hold a press at a point for `durationMs` and release. Hosts
  /// classify a held canvas pointer (down + minimal movement +
  /// elapsed time) as a long-press and dispatch via this entry
  /// point so adapters do not have to synthesise the gesture from
  /// a stream of `sendTouch` events.
  longPressAt(
    sessionId: string,
    point: {
      readonly x: number;
      readonly y: number;
      readonly srcWidth?: number;
      readonly srcHeight?: number;
    },
    durationMs: number,
  ): Promise<void>;

  /// Drag from `from` to `to` over `durationMs`. Used when the
  /// canvas pointer travels beyond the long-press tolerance — the
  /// adapter dispatches a single swipe rather than a sequence of
  /// touch events.
  swipe(
    sessionId: string,
    from: {
      readonly x: number;
      readonly y: number;
      readonly srcWidth?: number;
      readonly srcHeight?: number;
    },
    to: { readonly x: number; readonly y: number },
    durationMs: number,
  ): Promise<void>;

  /// Two-finger pinch centred on a normalised point. `fromScale` is
  /// the gesture's starting scale (1.0 when a trackpad pinch begins)
  /// and `toScale` its end scale (>1 zoom-in, <1 zoom-out). Routed
  /// outside the touch sink because pinch is multi-pointer; the
  /// adapter synthesises the two-finger gesture natively.
  pinch(
    sessionId: string,
    center: { readonly xRatio: number; readonly yRatio: number },
    fromScale: number,
    toScale: number,
    durationMs: number,
  ): Promise<void>;

  /// Type text into the device's focused field. The caller ensures
  /// a field is focused first (the mirror user taps it); this is a
  /// raw passthrough and does not focus or clear anything.
  inputText(sessionId: string, text: string): Promise<void>;

  /// Delete `count` characters backward from the device's cursor.
  eraseText(sessionId: string, count: number): Promise<void>;

  /// Press a single named key on the device (e.g. "enter").
  pressKey(sessionId: string, key: string): Promise<void>;

  /// Open a touch sink for the named session. Adapters return the
  /// implementation that fits their device channel (streaming raw
  /// events vs classified gesture dispatch); the canvas-side
  /// consumer drives it through the three-stage lifecycle without
  /// inspecting which kind it received.
  createTouchSink(sessionId: string): MirrorTouchSink;

  /// Update the session's reported encoded dimensions. The Rust SCK
  /// adapter hands the host a placeholder pair before the first
  /// frame arrives because it cannot know the encoder's output
  /// resolution upfront; the WebCodecs decoder is the first place
  /// the real dimensions are available, so the frontend reports
  /// them back here. UI consumers (phone-frame chrome, inspector
  /// overlay) rely on the corrected values for aspect-accurate
  /// layout and tap-coordinate math.
  setSessionDims(
    sessionId: string,
    width: number,
    height: number,
  ): void;
}
