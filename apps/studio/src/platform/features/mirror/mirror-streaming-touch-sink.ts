import type {
  MirrorSession,
  MirrorTouchSink,
  ScreenMirror,
  TouchPoint,
} from "../../../domain/features/mirror/index.js";

/**
 * Touch sink for backends that expose a streaming control channel
 * — pointer down / move / up are forwarded verbatim and the
 * device's input layer classifies the resulting gesture. scrcpy
 * is the canonical example: each `TYPE_INJECT_TOUCH_EVENT` flows
 * straight to the Android input subsystem, which decides between
 * tap, swipe, and long-press based on the timing it observes.
 *
 * The trajectory metadata supplied to `endPress` is unused — the
 * device sees every event the user produced and reaches its own
 * verdict.
 */
export class StreamingTouchSink implements MirrorTouchSink {
  constructor(
    private readonly mirror: ScreenMirror,
    private readonly session: MirrorSession,
  ) {}

  async beginPress(point: TouchPoint): Promise<void> {
    await this.mirror.sendTouch(this.session, {
      action: "down",
      x: point.x,
      y: point.y,
      pressure: 1,
      srcWidth: point.srcWidth,
      srcHeight: point.srcHeight,
    });
  }

  async trackTo(point: TouchPoint): Promise<void> {
    await this.mirror.sendTouch(this.session, {
      action: "move",
      x: point.x,
      y: point.y,
      pressure: 1,
      srcWidth: point.srcWidth,
      srcHeight: point.srcHeight,
    });
  }

  async endPress(point: TouchPoint): Promise<void> {
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
