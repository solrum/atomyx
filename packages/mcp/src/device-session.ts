import type { Driver, Clock, Logger } from "@atomyx/driver";
import { Orchestra, NoopLogger, SystemClock } from "@atomyx/driver";

/**
 * `DeviceSession` — the MCP server's active-device state.
 *
 * Contract: a single `atomyx-mcp` process can drive any connected
 * device on demand. No device is bound at startup; the agent
 * enumerates via `list_devices`, picks one via `select_device`,
 * and switches mid-session whenever it needs a different target.
 *
 *   - Server starts with NO active driver by default.
 *   - `list_devices` works without a device (host-side
 *     enumeration only).
 *   - `select_device({platform, id})` constructs the right driver,
 *     connects it, wires a fresh Orchestra, marks it current.
 *   - Every device-touching tool reads `ctx.session.current()`;
 *     when null the tool returns `{ok: false, reason: "no
 *     active device — call select_device first"}`.
 *   - `disconnect_device` tears down the current driver cleanly.
 *   - Re-calling `select_device` disconnects the previous driver
 *     and wires a new one — one session can drive iOS, Android,
 *     iOS again, etc. without a process restart.
 *
 * Driver construction is factory-based: the consumer that builds
 * the session (typically the CLI entry point) passes a map of
 * `platform → factory(id, options) → Driver`. This package stays
 * driver-agnostic; knowing about concrete driver classes is the
 * caller's responsibility, which keeps the session reusable by
 * any downstream consumer that needs the same ergonomics.
 */
export interface DriverFactory {
  (id: string, options: DriverSelectOptions): Driver;
}

export interface DriverSelectOptions {
  /** iOS only — simulator vs physical device. */
  readonly kind?: "simulator" | "device";
  /** iOS only — override TCP port. */
  readonly port?: number;
}

export interface SelectDeviceInput {
  readonly platform: "ios" | "android";
  readonly id: string;
  readonly kind?: "simulator" | "device";
  readonly port?: number;
}

export interface ActiveDevice {
  readonly platform: "ios" | "android";
  readonly id: string;
  readonly kind?: "simulator" | "device";
  readonly orchestra: Orchestra;
  readonly driver: Driver;
  readonly connectedAt: number;
}

export interface DeviceSessionDeps {
  readonly factories: Readonly<Record<"ios" | "android", DriverFactory>>;
  readonly clock?: Clock;
  readonly logger?: Logger;
}

export class DeviceSession {
  private active: ActiveDevice | null = null;
  private readonly factories: DeviceSessionDeps["factories"];
  private readonly clock: Clock;
  private readonly logger: Logger;

  constructor(deps: DeviceSessionDeps) {
    this.factories = deps.factories;
    this.clock = deps.clock ?? new SystemClock();
    this.logger = deps.logger ?? new NoopLogger();
  }

  /**
   * Return the currently-bound device + its Orchestra, or null
   * when no device is selected. Tools call this every time they
   * need to issue a command; the null case means "agent has not
   * called select_device yet" and is a recoverable state (tools
   * return `{ok: false, reason: ...}`, not throw).
   */
  current(): ActiveDevice | null {
    return this.active;
  }

  /** Convenience: true iff a device is selected + connected. */
  isActive(): boolean {
    return this.active !== null;
  }

  /**
   * Bind a new active device. Disconnects the previous one if
   * any, constructs the driver via the factory map, connects it,
   * wires a fresh Orchestra, and stores the result as
   * `current()`. Throws on factory errors or connect errors —
   * the `select_device` tool catches + converts to
   * `{ok: false, reason}`.
   */
  async select(input: SelectDeviceInput): Promise<ActiveDevice> {
    // Tear down the previous driver cleanly before switching.
    // Errors during disconnect are logged but do NOT block the
    // new selection — if the previous driver is already wedged
    // we still want the new one to come up.
    if (this.active) {
      const previous = this.active;
      this.active = null;
      try {
        await previous.driver.disconnect();
        this.logger.info("session.previous_disconnected", {
          id: previous.id,
          platform: previous.platform,
        });
      } catch (err) {
        this.logger.warn("session.previous_disconnect_failed", {
          id: previous.id,
          platform: previous.platform,
          error: (err as Error).message,
        });
      }
    }

    const factory = this.factories[input.platform];
    if (!factory) {
      throw new Error(
        `session.select: no driver factory registered for platform "${input.platform}"`,
      );
    }

    const driver = factory(input.id, { kind: input.kind, port: input.port });
    try {
      await driver.connect();
    } catch (err) {
      // Leave active null — the new driver didn't connect, so
      // we can't claim it as current. Propagate so select_device
      // can report a structured failure.
      throw new Error(
        `session.select: driver.connect failed for ${input.platform}/${input.id}: ${(err as Error).message}`,
      );
    }

    const orchestra = new Orchestra({
      driver,
      clock: this.clock,
      logger: this.logger,
    });

    const active: ActiveDevice = {
      platform: input.platform,
      id: input.id,
      kind: input.kind,
      orchestra,
      driver,
      connectedAt: Date.now(),
    };
    this.active = active;
    this.logger.info("session.device_selected", {
      platform: input.platform,
      id: input.id,
      kind: input.kind,
    });
    return active;
  }

  /**
   * Tear down the active driver and mark the session idle.
   * No-op when already idle. Used by `disconnect_device` tool
   * and by the server's SIGINT handler.
   */
  async disconnect(): Promise<void> {
    if (!this.active) return;
    const previous = this.active;
    this.active = null;
    try {
      await previous.driver.disconnect();
      this.logger.info("session.disconnected", {
        id: previous.id,
        platform: previous.platform,
      });
    } catch (err) {
      this.logger.warn("session.disconnect_failed", {
        id: previous.id,
        platform: previous.platform,
        error: (err as Error).message,
      });
    }
  }
}
