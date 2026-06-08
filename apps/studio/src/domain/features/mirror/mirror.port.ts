import type {
  ClipOptions,
  H264Frame,
  MirrorOptions,
  MirrorSession,
  MirrorTarget,
  RecordingHandle,
  TouchAction,
  Unsubscribe,
} from "./mirror.types.js";
import type { MirrorTouchSink } from "./mirror-touch-sink.js";

export interface TouchEvent {
  readonly action: TouchAction;
  /** Integer x in source pixel space (0..srcWidth or 0..videoWidth). */
  readonly x: number;
  /** Integer y in source pixel space (0..srcHeight or 0..videoHeight). */
  readonly y: number;
  /** Pressure 0..1. Use 1 for DOWN/MOVE and 0 for UP. */
  readonly pressure: number;
  /**
   * Source pixel width the caller used to compute `x`. Backends
   * that did not populate `MirrorSession.videoWidth` at start
   * (SCK helper learns dims from the first decoded frame) rely on
   * this hint to normalize x → ratio.
   */
  readonly srcWidth?: number;
  /** Source pixel height matching `srcWidth`. */
  readonly srcHeight?: number;
}

/**
 * Captures a device screen and delivers H.264 frames to Studio.
 * Mirror sits beside the runtime control surface, not inside it —
 * the `Driver` port sends commands; `ScreenMirror` observes. One
 * adapter per target kind; the dispatcher picks based on
 * `MirrorTarget.kind`.
 *
 * Platform notes (expand when adding support):
 *
 *   - android: adapter streams scrcpy-server H.264 NAL over a
 *     forwarded adb TCP socket.
 *   - ios-simulator: adapter records via `xcrun simctl io
 *     recordVideo` and decodes the MP4 back into NAL frames.
 *   - ios-device: adapter spawns ffmpeg against the CoreMediaIO
 *     AVFoundation input; macOS host only.
 *
 * Adapter guarantees:
 *
 *   - `start` resolves once the session is ready to emit frames.
 *   - `onFrame` listeners receive NAL units in decode order; the
 *     first keyframe after start is an IDR.
 *   - `record` captures the same stream to an MP4 on disk
 *     independently of attached frame listeners.
 *   - `stop` releases subprocess resources synchronously enough
 *     that a new session can start on the same target immediately.
 */
export interface ScreenMirror {
  start(target: MirrorTarget, opts?: MirrorOptions): Promise<MirrorSession>;
  stop(session: MirrorSession): Promise<void>;

  onFrame(
    session: MirrorSession,
    listener: (frame: H264Frame) => void,
  ): Unsubscribe;

  record(session: MirrorSession, outputPath: string): Promise<RecordingHandle>;
  stopRecording(handle: RecordingHandle): Promise<void>;
  clipFromRecording(handle: RecordingHandle, opts: ClipOptions): Promise<string>;

  /**
   * Inject a touch event. Adapters that cannot drive their target
   * advertise `capabilities.supportsTouch === false`; callers must
   * gate their pointer plumbing on that flag rather than relying on
   * this method to reject at runtime.
   */
  sendTouch(session: MirrorSession, event: TouchEvent): Promise<void>;

  /**
   * Press at a point and hold it for `durationMs` before releasing.
   * Maps onto the driver's `longPressAt` primitive — the host JS
   * classifies the gesture (touch hold without significant
   * movement) and routes it here so adapters can dispatch a single
   * native long-press rather than synthesising it from a stream of
   * `down`/`up` touch events.
   */
  longPressAt(
    session: MirrorSession,
    point: { readonly x: number; readonly y: number; readonly srcWidth?: number; readonly srcHeight?: number },
    durationMs: number,
  ): Promise<void>;

  /**
   * Drag from `from` to `to` over `durationMs`. Coordinates are in
   * the same space as the canvas (raw event x/y); adapters scale
   * to ratios using `srcWidth/srcHeight`.
   */
  swipe(
    session: MirrorSession,
    from: { readonly x: number; readonly y: number; readonly srcWidth?: number; readonly srcHeight?: number },
    to: { readonly x: number; readonly y: number },
    durationMs: number,
  ): Promise<void>;

  /**
   * Two-finger pinch centred on a normalised point. `fromScale` is
   * the gesture's starting scale (1.0 when a trackpad pinch begins)
   * and `toScale` its end scale (>1 zoom-in, <1 zoom-out). Distinct
   * from the single-pointer touch sink: pinch is inherently
   * multi-pointer, so it is dispatched as a standalone gesture
   * rather than routed through the sink's press lifecycle. Adapters
   * whose device channel cannot synthesise simultaneous pointers
   * reject at runtime.
   */
  pinch(
    session: MirrorSession,
    center: { readonly xRatio: number; readonly yRatio: number },
    fromScale: number,
    toScale: number,
    durationMs: number,
  ): Promise<void>;

  /**
   * Type text into the device's focused field. The caller is
   * responsible for ensuring a field is focused first (the mirror
   * user taps it); this is a raw passthrough to the device's
   * text-entry primitive and does not focus or clear anything.
   */
  inputText(session: MirrorSession, text: string): Promise<void>;

  /**
   * Delete backward from the device's focused field.
   *
   * Platform notes (expand when adding support):
   *
   *   - ios-simulator: deletes exactly `count` characters.
   *   - android: clears the whole focused field regardless of
   *     `count`. Callers needing precise per-character deletion
   *     (live keystroke streaming) gate on
   *     `capabilities.supportsLiveTyping`.
   */
  eraseText(session: MirrorSession, count: number): Promise<void>;

  /** Press a single named key on the device (e.g. "enter"). */
  pressKey(session: MirrorSession, key: string): Promise<void>;

  /**
   * Build the touch sink the canvas-side consumer feeds pointer
   * events into. Each adapter returns the sink shape that matches
   * its native channel — streaming sinks for backends whose device
   * input layer classifies gestures, classified sinks for backends
   * that expose dedicated tap / swipe / long-press primitives.
   *
   * Returning a sink (rather than asking the consumer to inspect a
   * capability flag) keeps the canvas free of platform branches:
   * the consumer calls the sink's three lifecycle methods
   * uniformly and the implementation choice rides with the
   * adapter that knows which paradigm fits its device channel.
   */
  createTouchSink(session: MirrorSession): MirrorTouchSink;
}
