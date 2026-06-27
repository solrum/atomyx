import type {
  Capabilities,
  DeviceInfo,
  Driver,
  ForegroundInfo,
  Gesture,
  InstalledApp,
  KeyCode,
  KeyResult,
  LaunchArgs,
  Point,
  Size,
  TreeNode,
} from "@atomyx/driver";
import type { IosDriverOptions } from "@atomyx/ios-driver";
import { IosDriver } from "@atomyx/ios-driver";
import { HidClient } from "./hid-client.js";

/** Options accepted by the IosSimDriver factory. */
export interface IosSimDriverOptions extends IosDriverOptions {
  /**
   * WS port emitted by the atomyx-sim-hid helper handshake.
   * Required when isSimDirectSupported() returns true; the sidecar
   * device factory starts the helper before calling createIosSimDriver.
   * May be omitted when the factory falls back to IosDriver.
   */
  readonly hidPort?: number;
}

/**
 * Capability interface for drivers that accept streaming touch
 * phase events (down / move / up) independent of gesture
 * classification. The sidecar checks for this interface before
 * routing streaming mirror input through the HID path.
 *
 * Only `IosSimDriver` implements this. The check is a type-guard,
 * not instanceof, so future alternative implementations can satisfy
 * the contract without class coupling.
 */
export interface StreamingTouchCapable {
  streamingTouchDown(point: Point, id: number): Promise<void>;
  streamingTouchMove(point: Point, id: number): Promise<void>;
  streamingTouchUp(point: Point, id: number): Promise<void>;
}

/** Type-guard used by the sidecar to detect streaming capability. */
export function isStreamingTouchCapable(
  driver: unknown,
): driver is StreamingTouchCapable {
  return (
    typeof driver === "object" &&
    driver !== null &&
    typeof (driver as StreamingTouchCapable).streamingTouchDown === "function" &&
    typeof (driver as StreamingTouchCapable).streamingTouchMove === "function" &&
    typeof (driver as StreamingTouchCapable).streamingTouchUp === "function"
  );
}

/**
 * iOS Simulator driver that routes touch gestures through the
 * atomyx-sim-hid helper while delegating every other Driver method
 * to an internally-held IosDriver (XCUITest-backed).
 *
 * Touch operations (tap, longPress, swipe, and streaming
 * down/move/up) call into HidClient over a localhost WebSocket
 * connection to the helper process. The helper translates each
 * message into a SimDeviceLegacyHIDClient dispatch.
 *
 * Non-touch operations (hierarchy, screenshot, app lifecycle, text
 * input) stay on the XCUITest path because the HID layer has no
 * equivalent.
 *
 * Callers obtain an IosDriver instead when isSimDirectSupported()
 * returns false — see createIosSimDriver().
 *
 * Platform notes:
 *   - iOS Simulator on arm64 + Xcode 26+: full HID path available.
 *   - All other configurations: factory returns IosDriver instead;
 *     this class is never instantiated.
 */
export class IosSimDriver implements Driver, StreamingTouchCapable {
  readonly platform = "ios" as const;

  private readonly delegate: IosDriver;
  private readonly hid: HidClient;
  // Logical-point screen size cached for pixel→normalized conversion.
  // The Driver port contract delivers Point in absolute logical
  // points (the XCUITest path forwards them verbatim); the sim-hid
  // helper expects [0..1] normalized coordinates, so this class
  // divides by screen size before dispatch.
  private cachedSize: Size | null = null;

  constructor(opts: IosSimDriverOptions) {
    if (opts.hidPort === undefined) {
      throw new Error(
        "IosSimDriver requires hidPort — start atomyx-sim-hid and pass its handshake port.",
      );
    }
    this.delegate = new IosDriver(opts);
    this.hid = new HidClient({ port: opts.hidPort });
  }

  private async normalize(point: Point): Promise<{ x: number; y: number }> {
    if (!this.cachedSize) {
      this.cachedSize = await this.delegate.screenSize();
    }
    const w = this.cachedSize.width > 0 ? this.cachedSize.width : 1;
    const h = this.cachedSize.height > 0 ? this.cachedSize.height : 1;
    return { x: point.x / w, y: point.y / h };
  }

  get capabilities(): Capabilities {
    return this.delegate.capabilities;
  }

  // ── Lifecycle ─────────────────────────────────────────────────

  async connect(): Promise<void> {
    await this.delegate.connect();
    await this.hid.connect();
  }

  async disconnect(): Promise<void> {
    await this.hid.dispose();
    await this.delegate.disconnect();
  }

  isConnected(): boolean {
    return this.delegate.isConnected();
  }

  // ── Hierarchy & state (delegated) ─────────────────────────────

  async hierarchy(): Promise<TreeNode> {
    return this.delegate.hierarchy();
  }

  async waitForIdle(timeoutMs: number): Promise<boolean> {
    return this.delegate.waitForIdle(timeoutMs);
  }

  // ── Touch gestures (HID path) ─────────────────────────────────

  async tap(point: Point): Promise<void> {
    await this.hid.tap(await this.normalize(point));
  }

  async longPress(point: Point, durationMs: number): Promise<void> {
    // Hold at one point for the given duration. HidClient.tap with
    // a long holdMs matches the helper's hold-then-release gesture.
    await this.hid.tap(await this.normalize(point), durationMs);
  }

  async swipe(from: Point, to: Point, durationMs: number): Promise<void> {
    const [nFrom, nTo] = await Promise.all([
      this.normalize(from),
      this.normalize(to),
    ]);
    await this.hid.swipe(nFrom, nTo, durationMs);
  }

  async dispatchGesture(gesture: Gesture): Promise<void> {
    // Multi-pointer gestures require coordinated HID sequencing
    // that the current helper does not expose. Delegate to
    // IosDriver so the XCUITest path handles the full gesture.
    return this.delegate.dispatchGesture(gesture);
  }

  // ── Streaming touch (StreamingTouchCapable) ───────────────────

  async streamingTouchDown(point: Point, id: number): Promise<void> {
    await this.hid.touchDown(await this.normalize(point), id);
  }

  async streamingTouchMove(point: Point, id: number): Promise<void> {
    await this.hid.touchMove(await this.normalize(point), id);
  }

  async streamingTouchUp(point: Point, id: number): Promise<void> {
    await this.hid.touchUp(await this.normalize(point), id);
  }

  // ── Text input (delegated — XCUITest path) ───────────────────

  async inputText(text: string): Promise<void> {
    return this.delegate.inputText(text);
  }

  async eraseText(count: number): Promise<void> {
    return this.delegate.eraseText(count);
  }

  async pressKey(key: KeyCode): Promise<KeyResult> {
    return this.delegate.pressKey(key);
  }

  async hideKeyboard(): Promise<KeyResult> {
    return this.delegate.hideKeyboard();
  }

  // ── App lifecycle (delegated) ──────────────────────────────────

  async launchApp(bundleId: string, args?: LaunchArgs): Promise<void> {
    return this.delegate.launchApp(bundleId, args);
  }

  async stopApp(bundleId: string): Promise<void> {
    return this.delegate.stopApp(bundleId);
  }

  async killApp(bundleId: string): Promise<void> {
    return this.delegate.killApp(bundleId);
  }

  async currentForeground(): Promise<ForegroundInfo> {
    return this.delegate.currentForeground();
  }

  async listApps(): Promise<readonly InstalledApp[]> {
    return this.delegate.listApps();
  }

  // ── Media + device info (delegated) ───────────────────────────

  async screenshot(): Promise<Uint8Array> {
    return this.delegate.screenshot();
  }

  async deviceInfo(): Promise<DeviceInfo> {
    return this.delegate.deviceInfo();
  }

  async screenSize(): Promise<Size> {
    return this.delegate.screenSize();
  }
}
