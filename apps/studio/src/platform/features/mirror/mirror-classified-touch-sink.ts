import {
  classifyGesture,
  LONG_PRESS_MS,
  type MirrorSession,
  type MirrorTouchSink,
  type ScreenMirror,
  type TouchPoint,
} from "../../../domain/features/mirror/index.js";

/**
 * Touch sink for backends that expose dedicated tap / swipe /
 * long-press primitives — simctl is the canonical example. The
 * device cannot accept a streaming touch channel, so the host
 * classifies the gesture from the completed pointer trajectory
 * and dispatches the matching adapter API.
 *
 * `beginPress` records the start point so the eventual swipe call
 * has both endpoints; `trackTo` is a no-op because the gesture
 * decision is made at release time from the metadata the canvas
 * already tracks. `endPress` looks at `displacementPx` and
 * `heldMs` and routes to one of three adapter calls.
 */
export class ClassifiedTouchSink implements MirrorTouchSink {
  private start: TouchPoint | null = null;

  constructor(
    private readonly mirror: ScreenMirror,
    private readonly session: MirrorSession,
  ) {}

  async beginPress(point: TouchPoint): Promise<void> {
    this.start = point;
  }

  async trackTo(): Promise<void> {
    // Intentionally empty — the classified-gesture path defers
    // every dispatch to `endPress`. The canvas keeps pointer
    // capture across moves through React's pointer-event model;
    // no per-move work is needed here.
  }

  async endPress(
    point: TouchPoint,
    heldMs: number,
    displacementPx: number,
  ): Promise<void> {
    const start = this.start;
    this.start = null;
    if (start === null) return;
    const gesture = classifyGesture(displacementPx, heldMs);
    if (gesture === "swipe") {
      await this.mirror.swipe(
        this.session,
        {
          x: start.x,
          y: start.y,
          srcWidth: start.srcWidth,
          srcHeight: start.srcHeight,
        },
        { x: point.x, y: point.y },
        Math.max(80, Math.min(2_000, Math.round(heldMs))),
      );
      return;
    }
    if (gesture === "long-press") {
      await this.mirror.longPressAt(
        this.session,
        {
          x: start.x,
          y: start.y,
          srcWidth: start.srcWidth,
          srcHeight: start.srcHeight,
        },
        Math.max(LONG_PRESS_MS, Math.min(5_000, Math.round(heldMs))),
      );
      return;
    }
    await this.mirror.sendTouch(this.session, {
      action: "up",
      x: point.x,
      y: point.y,
      pressure: 0,
      srcWidth: point.srcWidth,
      srcHeight: point.srcHeight,
    });
  }
}
