import type { Driver, Clock, Logger } from "@atomyx/core-driver";
import { Orchestra, NoopLogger, SystemClock } from "@atomyx/core-driver";

/**
 * `DeviceSession` — the MCP server's active-device state.
 *
 * Rationale: a single `atomyx-mcp` process should be able to
 * drive any connected device on demand, not commit to one at
 * startup. Pre-refactor, the legacy `src/` MCP server exposed a
 * `select_device` tool so an agent could enumerate devices then
 * pick one mid-session; when the iOS and Android adapters were
 * split into `@atomyx/core-driver-{ios,android}`, that runtime
 * selection capability was lost — each MCP process had to be
 * launched with `--platform X --device Y`, forcing users to
 * maintain separate `.mcp.json` entries for every platform they
 * wanted to drive. `DeviceSession` restores the pre-refactor
 * ergonomics on top of the new architecture:
 *
 *   - Server starts with NO active driver by default
 *   - `list_devices` tool works without a device (host-side
 *     enumeration only)
 *   - Agent calls `select_device({platform, id})` → session
 *     constructs the right driver, connects it, wires a fresh
 *     Orchestra, marks it current
 *   - Every device-touching tool reads `ctx.session.current()`;
 *     when null the tool returns `{ok: false, reason: "no
 *     active device — call select_device first"}`
 *   - `disconnect_device` tears down the current driver cleanly
 *   - Re-calling `select_device` disconnects the previous
 *     driver and wires a new one — one session can drive
 *     iOS then Android then iOS again without restart
 *
 * The class is intentionally driver-factory-based: the `bin.ts`
 * that constructs the session passes a map of `platform →
 * factory(id, options) → Driver`, so `core-driver-mcp` itself
 * stays driver-agnostic. The binary (which KNOWS about concrete
 * IosDriver + AndroidDriver) builds the factory map; any other
 * consumer (Studio, test-mgmt) can plug in their own drivers the
 * same way.
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
