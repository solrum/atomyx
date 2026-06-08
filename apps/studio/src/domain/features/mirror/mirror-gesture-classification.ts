/**
 * Pure gesture classifier for the mirror canvas. The canvas is
 * driven by a single pointer and one of three high-level gestures
 * is dispatched to the device on release. Streaming raw down/move
 * /up to the iOS adapter is rejected unless the host first
 * synthesises a real `XCUICoordinate`-bound gesture, so we
 * classify here and the adapter receives the high-level command.
 *
 * Thresholds:
 *   - `TAP_TOLERANCE_PX`: max client-pixel travel still counted
 *     as stationary. Beyond this the gesture is a swipe.
 *   - `LONG_PRESS_MS`: minimum hold duration required to elevate
 *     a stationary press from tap to long-press.
 */
export const TAP_TOLERANCE_PX = 8;
export const LONG_PRESS_MS = 500;

export type Gesture = "tap" | "long-press" | "swipe";

/**
 * Classify a finished pointer gesture from its total displacement
 * (Euclidean, in canvas/client pixels) and how long the pointer
 * was held down. Tap and long-press both require the pointer to
 * have stayed within `TAP_TOLERANCE_PX`; long-press additionally
 * requires `heldMs ≥ LONG_PRESS_MS`. Anything that travelled
 * further is a swipe regardless of hold time.
 */
export function classifyGesture(
  displacementPx: number,
  heldMs: number,
): Gesture {
  if (displacementPx > TAP_TOLERANCE_PX) return "swipe";
  if (heldMs >= LONG_PRESS_MS) return "long-press";
  return "tap";
}
