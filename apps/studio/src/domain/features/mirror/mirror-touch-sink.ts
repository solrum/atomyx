/**
 * Polymorphic touch dispatcher between the mirror canvas and a
 * mirror adapter. Two paradigms exist for relaying user input
 * from the host to a device:
 *
 *   - Streaming backends (e.g. scrcpy) forward each pointer event
 *     verbatim and rely on the device's native input subsystem to
 *     classify the resulting gesture (tap / swipe / long-press).
 *   - Classified backends (e.g. simctl) expect the host to
 *     classify the gesture from the completed pointer trajectory
 *     and dispatch a dedicated API per gesture.
 *
 * Both paradigms satisfy the same three-stage contract below:
 *
 *   1. `beginPress` — the user put a finger down on the canvas.
 *   2. `trackTo` — the pointer moved while still pressed.
 *   3. `endPress` — the user released the press.
 *
 * Adapters return the implementation that matches their device
 * channel; the canvas-side consumer calls these three methods
 * uniformly and never inspects which kind of sink it has.
 *
 * `endPress` carries the trajectory metadata (`heldMs`,
 * `displacementPx`) so a classified sink can decide between tap,
 * swipe, and long-press without re-tracking pointer history. A
 * streaming sink ignores those numbers.
 */
export interface TouchPoint {
  readonly x: number;
  readonly y: number;
  /**
   * Source frame width / height the coordinates are expressed in.
   * Adapters scale by `videoWidth/srcWidth` (and analogous for y)
   * before sending events on the wire.
   */
  readonly srcWidth: number;
  readonly srcHeight: number;
}

export interface MirrorTouchSink {
  /**
   * Press began at `point`. Streaming sinks emit a `down` on the
   * device immediately; classified sinks record the start and
   * defer dispatch until `endPress`.
   */
  beginPress(point: TouchPoint): Promise<void>;

  /**
   * Pointer moved to `point` while still pressed. Streaming sinks
   * emit a `move`; classified sinks no-op and rely on the
   * trajectory metadata supplied to `endPress`.
   */
  trackTo(point: TouchPoint): Promise<void>;

  /**
   * Pointer released at `point`. `heldMs` is the total press
   * duration in milliseconds; `displacementPx` is the Euclidean
   * canvas-pixel distance between the original press point and
   * `point`. Classified sinks use these to pick tap / swipe /
   * long-press; streaming sinks emit an `up` and ignore them.
   */
  endPress(
    point: TouchPoint,
    heldMs: number,
    displacementPx: number,
  ): Promise<void>;
}
