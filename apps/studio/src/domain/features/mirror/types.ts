/**
 * Shared types for the screen-mirror feature. Mirror identifies
 * targets by capability (OS + whether the device is a simulator
 * or a real one) because the right adapter is chosen from that
 * capability, not from how the rest of Studio models devices.
 */

export type MirrorTargetKind = "android" | "ios-simulator" | "ios-device";

export interface MirrorTarget {
  readonly id: string;
  readonly kind: MirrorTargetKind;
  readonly displayName: string;
}

export interface MirrorOptions {
  /** Upper-bound hint for encoded bitrate (bits/second). */
  readonly bitrate?: number;
  /** Longest edge in pixels. Adapters may downscale to hit this. */
  readonly maxSize?: number;
  readonly orientation?: "portrait" | "landscape";
}

/**
 * Feature switches reported by the adapter that produced the
 * session. Consumers read these to gate UI affordances — there is
 * no runtime attempt-and-fail dance.
 */
export interface MirrorCapabilities {
  /** `record()` / `stopRecording()` / `clipFromRecording()` work. */
  readonly supportsRecording: boolean;
  /**
   * The adapter can drive the device's input layer at all. UI
   * code gates pointer plumbing on this flag; the actual
   * paradigm (streaming raw events vs classifying gestures) is
   * abstracted by `ScreenMirror.createTouchSink`.
   */
  readonly supportsTouch: boolean;
  /**
   * `inputText` / `pressKey` reach the device's focused field, so
   * the local text-insert affordance (compose-then-commit) works.
   * UI gates the insert field on this flag.
   */
  readonly supportsKeyboard: boolean;
  /**
   * `eraseText` deletes exactly `count` characters backward,
   * making per-keystroke live typing (the hidden-input diff
   * mirror) safe. False when the backend can only clear the whole
   * focused field — live typing would wipe the buffer on the
   * first correction, so only the commit-on-Enter insert path is
   * offered.
   */
  readonly supportsLiveTyping: boolean;
  /** `pinch()` can synthesise a simultaneous two-finger gesture. */
  readonly supportsPinch: boolean;
}

export interface MirrorSession {
  readonly id: string;
  readonly target: MirrorTarget;
  readonly startedAt: number;
  /** Adapter name — opaque to consumers, useful for telemetry. */
  readonly backend: string;
  /**
   * Encoded frame dimensions reported by the adapter. For touch
   * injection the device-side coordinate system matches these
   * numbers, not the rendered `<video>` size on the host.
   */
  readonly videoWidth: number;
  readonly videoHeight: number;
  readonly capabilities: MirrorCapabilities;
}

export type TouchAction = "down" | "up" | "move";

export interface H264Frame {
  readonly nal: Uint8Array;
  /** Presentation timestamp in microseconds since session start. */
  readonly timestampUs: number;
  readonly keyframe: boolean;
}

export interface RecordingHandle {
  readonly session: MirrorSession;
  readonly outputPath: string;
}

export interface ClipOptions {
  readonly startMs: number;
  readonly endMs: number;
  readonly outputPath: string;
}

export type Unsubscribe = () => void;
