import type { Gesture, KeyCode, Point } from "@atomyx/driver";
import type { Session } from "../../infra/session/session.js";

export interface InputServiceDeps {
  readonly session: Session;
}

// Half-gap between the two synthesised pinch fingers at scale 1.0,
// as a fraction of the device's shorter screen edge. The fingers
// start at center ± (this × fromScale) and end at center ±
// (this × toScale), so the gap grows on zoom-in and shrinks on
// zoom-out. Chosen so a 1.0→2.0 pinch spans a comfortable portion
// of the screen without either finger leaving the viewport.
const PINCH_HALF_GAP_FRACTION = 0.18;

/**
 * Coordinate-based input dispatch on the currently selected device.
 *
 * Designed for the Studio mirror canvas: callers only know
 * normalized ratios (0..1 across the viewport) because the canvas
 * dimensions are decoupled from the device's logical point space.
 * The service queries the driver for `screenSize()` (logical
 * points) and converts ratios to absolute coordinates before
 * dispatching the tap.
 *
 * `screenSize()` is cached per device selection — the value is
 * stable for the device lifetime. `tap()` reuses the cached size
 * to keep input round-trips short.
 */
export class InputService {
  private readonly session: Session;
  private cachedScreen: { readonly deviceId: string; readonly width: number; readonly height: number } | null = null;

  constructor(deps: InputServiceDeps) {
    this.session = deps.session;
  }

  async tapRatio(params: {
    readonly xRatio: number;
    readonly yRatio: number;
    /**
     * Bundle id to attach to before dispatching the tap. When set,
     * the service calls `launchApp(bundleId, { noReset: true })` so
     * the driver has an app reference to dispatch the tap against —
     * iOS XCUITest cannot tap without one. Adapters that already
     * track this bundle short-circuit the launch, preserving the
     * app's in-memory state. Mirror tap forwarding uses this to
     * drive a running app without resetting it.
     */
    readonly bundleId?: string;
  }): Promise<void> {
    const screen = await this.getScreen();
    const x = clamp(Math.round(params.xRatio * screen.width), 0, screen.width - 1);
    const y = clamp(Math.round(params.yRatio * screen.height), 0, screen.height - 1);
    const { orchestra } = this.session.requireDevice();
    if (params.bundleId !== undefined && params.bundleId.length > 0) {
      await orchestra.launchApp(params.bundleId, { noReset: true });
    }
    await orchestra.tapAt({ x, y });
  }

  async longPressRatio(params: {
    readonly xRatio: number;
    readonly yRatio: number;
    /** Press duration in milliseconds. Defaults to 500ms. */
    readonly durationMs?: number;
    readonly bundleId?: string;
  }): Promise<void> {
    const screen = await this.getScreen();
    const x = clamp(Math.round(params.xRatio * screen.width), 0, screen.width - 1);
    const y = clamp(Math.round(params.yRatio * screen.height), 0, screen.height - 1);
    const { orchestra } = this.session.requireDevice();
    if (params.bundleId !== undefined && params.bundleId.length > 0) {
      await orchestra.launchApp(params.bundleId, { noReset: true });
    }
    await orchestra.longPressAt({ x, y }, params.durationMs ?? 500);
  }

  async swipeRatio(params: {
    readonly fromXRatio: number;
    readonly fromYRatio: number;
    readonly toXRatio: number;
    readonly toYRatio: number;
    /** Total swipe duration in milliseconds. Defaults to 200ms. */
    readonly durationMs?: number;
    readonly bundleId?: string;
  }): Promise<void> {
    const screen = await this.getScreen();
    const fromX = clamp(Math.round(params.fromXRatio * screen.width), 0, screen.width - 1);
    const fromY = clamp(Math.round(params.fromYRatio * screen.height), 0, screen.height - 1);
    const toX = clamp(Math.round(params.toXRatio * screen.width), 0, screen.width - 1);
    const toY = clamp(Math.round(params.toYRatio * screen.height), 0, screen.height - 1);
    const { orchestra } = this.session.requireDevice();
    if (params.bundleId !== undefined && params.bundleId.length > 0) {
      await orchestra.launchApp(params.bundleId, { noReset: true });
    }
    await orchestra.swipeAt({ x: fromX, y: fromY }, { x: toX, y: toY }, params.durationMs ?? 200);
  }

  /**
   * Two-finger pinch centred on a normalised point. `fromScale` is
   * the gesture's starting scale (1.0 when a trackpad pinch begins)
   * and `toScale` its end scale (>1 zoom-in, <1 zoom-out). The two
   * fingers are placed symmetrically on the horizontal axis around
   * the centre; their gap is proportional to scale, so the device
   * sees them spread apart (zoom-in) or close together (zoom-out).
   *
   * Requires the driver's `canMultiPointer` capability; the driver
   * rejects the two-pointer gesture otherwise and the error
   * propagates to the caller.
   */
  async pinchRatio(params: {
    readonly centerXRatio: number;
    readonly centerYRatio: number;
    readonly fromScale: number;
    readonly toScale: number;
    /** Total pinch duration in milliseconds. Defaults to 250ms. */
    readonly durationMs?: number;
    readonly bundleId?: string;
  }): Promise<void> {
    const screen = await this.getScreen();
    const cx = clamp(Math.round(params.centerXRatio * screen.width), 0, screen.width - 1);
    const cy = clamp(Math.round(params.centerYRatio * screen.height), 0, screen.height - 1);
    const baseHalfGap = Math.min(screen.width, screen.height) * PINCH_HALF_GAP_FRACTION;
    const fromGap = baseHalfGap * params.fromScale;
    const toGap = baseHalfGap * params.toScale;
    const durationSeconds = Math.max(1, params.durationMs ?? 250) / 1000;

    const left = (gap: number): Point => ({
      x: clamp(Math.round(cx - gap), 0, screen.width - 1),
      y: cy,
    });
    const right = (gap: number): Point => ({
      x: clamp(Math.round(cx + gap), 0, screen.width - 1),
      y: cy,
    });

    const gesture: Gesture = {
      pointers: [
        {
          id: "pinch-a",
          waypoints: [
            { phase: "down", point: left(fromGap), atOffsetSeconds: 0 },
            { phase: "move", point: left(toGap), atOffsetSeconds: durationSeconds },
            { phase: "up", point: left(toGap), atOffsetSeconds: durationSeconds },
          ],
        },
        {
          id: "pinch-b",
          waypoints: [
            { phase: "down", point: right(fromGap), atOffsetSeconds: 0 },
            { phase: "move", point: right(toGap), atOffsetSeconds: durationSeconds },
            { phase: "up", point: right(toGap), atOffsetSeconds: durationSeconds },
          ],
        },
      ],
    };

    const { orchestra } = this.session.requireDevice();
    if (params.bundleId !== undefined && params.bundleId.length > 0) {
      await orchestra.launchApp(params.bundleId, { noReset: true });
    }
    await orchestra.dispatchGesture(gesture);
  }

  /**
   * Type text into the focused field on the device. Assumes a field
   * is already focused (the mirror user taps it first); does not
   * focus or clear anything itself — raw passthrough to the driver's
   * text-entry primitive.
   */
  async inputText(text: string): Promise<void> {
    const { orchestra } = this.session.requireDevice();
    await orchestra.typeText(text);
  }

  /** Delete `count` characters backward from the cursor. */
  async eraseText(count: number): Promise<void> {
    if (count <= 0) return;
    const { orchestra } = this.session.requireDevice();
    // Send `count` delete keystrokes via the text path. The driver's
    // eraseText primitive clears the whole field (select-all + delete
    // semantics), which is wrong for a per-keystroke Backspace; the
    // delete control character U+0008 is interpreted as a single
    // backward delete by the device's text-entry layer.
    await orchestra.typeText("\u0008".repeat(count));
  }

  /** Press a single named key (e.g. "enter"). */
  async pressKey(key: KeyCode): Promise<void> {
    const { orchestra } = this.session.requireDevice();
    await orchestra.pressKey(key);
  }

  private async getScreen(): Promise<{ readonly width: number; readonly height: number }> {
    const device = this.session.requireDevice();
    if (this.cachedScreen && this.cachedScreen.deviceId === device.id) {
      return { width: this.cachedScreen.width, height: this.cachedScreen.height };
    }
    const size = await device.driver.screenSize();
    this.cachedScreen = { deviceId: device.id, width: size.width, height: size.height };
    return size;
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}
