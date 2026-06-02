import type { Driver, Orchestra } from "@atomyx/driver";

export interface DeviceSelection {
  readonly id: string;
  readonly platform: "android" | "ios";
  readonly driver: Driver;
  readonly orchestra: Orchestra;
  /**
   * Tear-down hook. Called when the session deselects this device
   * or when the sidecar exits. Must be safe to call multiple times.
   */
  readonly dispose: () => Promise<void>;
}

/**
 * Minimal per-sidecar state container. Holds the currently selected
 * device (with its Orchestra instance + cleanup) and the id of an
 * in-flight run so handlers can look them up without passing state
 * through parameter lists.
 *
 * Pure state — no IO, no business logic. Services receive a
 * reference and mutate / read it. Keeps us off a god object while
 * still giving request handlers a single place to coordinate.
 */
export class Session {
  private device: DeviceSelection | null = null;
  private activeRunId: string | null = null;

  getDevice(): DeviceSelection | null {
    return this.device;
  }

  requireDevice(): DeviceSelection {
    if (!this.device) {
      throw new Error("No device selected");
    }
    return this.device;
  }

  async setDevice(selection: DeviceSelection | null): Promise<void> {
    if (this.device && this.device.id === selection?.id) {
      return;
    }
    const previous = this.device;
    this.device = selection;
    if (previous) {
      await previous.dispose().catch(() => {});
    }
  }

  getActiveRunId(): string | null {
    return this.activeRunId;
  }

  setActiveRunId(id: string | null): void {
    this.activeRunId = id;
  }

  async dispose(): Promise<void> {
    const previous = this.device;
    this.device = null;
    this.activeRunId = null;
    if (previous) {
      await previous.dispose().catch(() => {});
    }
  }
}
