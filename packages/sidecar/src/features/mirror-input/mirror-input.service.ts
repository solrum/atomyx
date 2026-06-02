import type { Session } from "../../infra/session/session.js";
import type { DeviceService } from "../device/index.js";
import type { InputService } from "../input/index.js";
import type { IosAgentService } from "../ios-agent/index.js";
import type { AndroidAgentService } from "../android-agent/index.js";
import { DriverNotReadyError, StreamingTouchNotSupportedError } from "./mirror-input.errors.js";
import { isStreamingTouchCapable } from "@atomyx/ios-sim-driver";

export { StreamingTouchNotSupportedError } from "./mirror-input.errors.js";

export interface MirrorInputServiceDeps {
  readonly deviceService: DeviceService;
  readonly inputService: InputService;
  readonly session: Session;
  readonly iosAgentService: IosAgentService;
  readonly androidAgentService: AndroidAgentService;
}

/**
 * Atomic `select device + tap ratio` for the Studio mirror canvas.
 *
 * Mirror taps arrive from the Rust command layer keyed by a
 * `deviceId` (the mirror session owns the device regardless of
 * which device the sidecar's `Session` currently points at). A
 * naive implementation — `selectDevice` then `tapRatio` as two
 * RPCs — races with any other handler that also calls
 * `selectDevice` (e.g. a script run, a device picker). Between
 * the two calls the session's selected device may flip, sending
 * the tap to the wrong target.
 *
 * This service serializes the pair behind a single promise chain
 * so concurrent mirror taps never interleave their selection and
 * dispatch. Selection is idempotent — the underlying
 * `DeviceService.select` short-circuits when the session is
 * already on the requested device.
 *
 * Readiness gate: each action checks whether the platform agent
 * (XCUITest for iOS, APK agent for Android) reports `state ===
 * "ready"` before dispatching. Taps that arrive while the agent is
 * still starting up are rejected with `DriverNotReadyError` so they
 * do not queue and replay as ghost inputs when the agent comes up.
 *
 * Streaming touch methods (streamingDown / streamingMove /
 * streamingUp) additionally check that the underlying driver
 * implements StreamingTouchCapable. When it does not,
 * `StreamingTouchNotSupportedError` is thrown so the UI can fall
 * back to the classified touch path.
 */
export class MirrorInputService {
  private readonly deviceService: DeviceService;
  private readonly inputService: InputService;
  private readonly session: Session;
  private readonly iosAgentService: IosAgentService;
  private readonly androidAgentService: AndroidAgentService;
  private queue: Promise<void> = Promise.resolve();

  constructor(deps: MirrorInputServiceDeps) {
    this.deviceService = deps.deviceService;
    this.inputService = deps.inputService;
    this.session = deps.session;
    this.iosAgentService = deps.iosAgentService;
    this.androidAgentService = deps.androidAgentService;
  }

  async tapRatio(params: {
    readonly deviceId: string;
    readonly xRatio: number;
    readonly yRatio: number;
    readonly bundleId?: string;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      await this.inputService.tapRatio({
        xRatio: params.xRatio,
        yRatio: params.yRatio,
        bundleId: params.bundleId,
      });
    });
  }

  async longPressRatio(params: {
    readonly deviceId: string;
    readonly xRatio: number;
    readonly yRatio: number;
    readonly durationMs?: number;
    readonly bundleId?: string;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      await this.inputService.longPressRatio({
        xRatio: params.xRatio,
        yRatio: params.yRatio,
        durationMs: params.durationMs,
        bundleId: params.bundleId,
      });
    });
  }

  async swipeRatio(params: {
    readonly deviceId: string;
    readonly fromXRatio: number;
    readonly fromYRatio: number;
    readonly toXRatio: number;
    readonly toYRatio: number;
    readonly durationMs?: number;
    readonly bundleId?: string;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      await this.inputService.swipeRatio({
        fromXRatio: params.fromXRatio,
        fromYRatio: params.fromYRatio,
        toXRatio: params.toXRatio,
        toYRatio: params.toYRatio,
        durationMs: params.durationMs,
        bundleId: params.bundleId,
      });
    });
  }

  async pinchRatio(params: {
    readonly deviceId: string;
    readonly centerXRatio: number;
    readonly centerYRatio: number;
    readonly fromScale: number;
    readonly toScale: number;
    readonly durationMs?: number;
    readonly bundleId?: string;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      await this.inputService.pinchRatio({
        centerXRatio: params.centerXRatio,
        centerYRatio: params.centerYRatio,
        fromScale: params.fromScale,
        toScale: params.toScale,
        durationMs: params.durationMs,
        bundleId: params.bundleId,
      });
    });
  }

  // ── Keyboard / text input ─────────────────────────────────────

  async inputText(params: {
    readonly deviceId: string;
    readonly text: string;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      await this.inputService.inputText(params.text);
    });
  }

  async eraseText(params: {
    readonly deviceId: string;
    readonly count: number;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      await this.inputService.eraseText(params.count);
    });
  }

  async pressKey(params: {
    readonly deviceId: string;
    readonly key: string;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      await this.inputService.pressKey(params.key);
    });
  }

  // ── Streaming touch (HID direct path) ─────────────────────────

  async streamingDown(params: {
    readonly deviceId: string;
    readonly xRatio: number;
    readonly yRatio: number;
    readonly touchId: number;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      const driver = this.requireStreamingDriver(params.deviceId);
      await driver.streamingTouchDown(
        { x: params.xRatio, y: params.yRatio },
        params.touchId,
      );
    });
  }

  async streamingMove(params: {
    readonly deviceId: string;
    readonly xRatio: number;
    readonly yRatio: number;
    readonly touchId: number;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      const driver = this.requireStreamingDriver(params.deviceId);
      await driver.streamingTouchMove(
        { x: params.xRatio, y: params.yRatio },
        params.touchId,
      );
    });
  }

  async streamingUp(params: {
    readonly deviceId: string;
    readonly xRatio: number;
    readonly yRatio: number;
    readonly touchId: number;
  }): Promise<void> {
    return this.serialize(async () => {
      await this.deviceService.select(params.deviceId);
      this.assertAgentReady(params.deviceId);
      const driver = this.requireStreamingDriver(params.deviceId);
      await driver.streamingTouchUp(
        { x: params.xRatio, y: params.yRatio },
        params.touchId,
      );
    });
  }

  /**
   * Checks the agent readiness for the currently selected device.
   * Throws `DriverNotReadyError` when the agent has not yet reached
   * `"ready"` state, preventing taps from queuing and replaying as
   * ghost inputs once the agent starts.
   *
   * Called after `DeviceService.select` so `session.getDevice()` is
   * guaranteed to reflect the target device.
   */
  private assertAgentReady(deviceId: string): void {
    const device = this.session.getDevice();
    if (!device) {
      throw new DriverNotReadyError(
        `device-id not ready: no device selected (id=${deviceId})`,
      );
    }
    if (device.platform === "ios") {
      const status = this.iosAgentService.status(deviceId);
      if (status.state !== "ready") {
        throw new DriverNotReadyError(
          `device-id not ready: state=${status.state} (id=${deviceId})`,
        );
      }
    } else {
      const status = this.androidAgentService.status(deviceId);
      if (status.state !== "ready") {
        throw new DriverNotReadyError(
          `device-id not ready: state=${status.state} (id=${deviceId})`,
        );
      }
    }
  }

  /**
   * Returns the current session driver if it implements
   * StreamingTouchCapable. Throws StreamingTouchNotSupportedError
   * when the driver does not support phase-by-phase events, so the
   * UI can fall back to the classified touch path without crashing.
   */
  private requireStreamingDriver(deviceId: string): import("@atomyx/ios-sim-driver").StreamingTouchCapable {
    const device = this.session.getDevice();
    if (!device) {
      throw new DriverNotReadyError(
        `streaming touch: no device selected (id=${deviceId})`,
      );
    }
    if (!isStreamingTouchCapable(device.driver)) {
      throw new StreamingTouchNotSupportedError(
        `streaming touch not supported for this device (id=${deviceId}). ` +
          "The HID path requires a simulator on arm64 + Xcode 26+.",
      );
    }
    return device.driver;
  }

  private serialize<T>(run: () => Promise<T>): Promise<T> {
    const next = this.queue.then(run, run);
    this.queue = next.then(
      () => undefined,
      () => undefined,
    );
    return next;
  }
}
