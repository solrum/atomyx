import type { EventBus } from "../../infra/events/event-bus.js";
import type { Session } from "../../infra/session/session.js";
import type { DeviceProbe } from "./device.probe.js";
import type { DriverFactory } from "./device.driver-factory.js";
import type { DeviceDescriptor } from "./device.types.js";

export interface DeviceServiceDeps {
  readonly probe: DeviceProbe;
  readonly factory: DriverFactory;
  readonly session: Session;
  readonly events: EventBus;
}

/**
 * Orchestrates device lifecycle — list, select, deselect — without
 * owning driver construction (DriverFactory) or discovery
 * (DeviceProbe). Publishes events via EventBus so the transport
 * layer stays free to forward them any way it wants.
 */
export class DeviceService {
  private readonly probe: DeviceProbe;
  private readonly factory: DriverFactory;
  private readonly session: Session;
  private readonly events: EventBus;
  private lastSnapshot: readonly DeviceDescriptor[] = [];

  constructor(deps: DeviceServiceDeps) {
    this.probe = deps.probe;
    this.factory = deps.factory;
    this.session = deps.session;
    this.events = deps.events;
  }

  async list(): Promise<readonly DeviceDescriptor[]> {
    const snapshot = await this.probe.scan();
    this.diffAndEmit(this.lastSnapshot, snapshot);
    this.lastSnapshot = snapshot;
    return snapshot;
  }

  async select(id: string): Promise<DeviceDescriptor> {
    const device = this.lastSnapshot.find((d) => d.id === id);
    if (!device) {
      // Force a rescan — caller may have skipped list() before select.
      const refreshed = await this.list();
      const found = refreshed.find((d) => d.id === id);
      if (!found) {
        throw new Error(`Device "${id}" not found`);
      }
      return this.selectKnown(found);
    }
    return this.selectKnown(device);
  }

  async deselect(): Promise<void> {
    await this.session.setDevice(null);
  }

  private async selectKnown(device: DeviceDescriptor): Promise<DeviceDescriptor> {
    const current = this.session.getDevice();
    if (current && current.id === device.id && current.driver.isConnected()) {
      return device;
    }
    if (current) {
      await current.dispose().catch(() => {});
      await this.session.setDevice(null);
    }
    const { driver, orchestra } = await this.factory.build(device);
    await this.session.setDevice({
      id: device.id,
      platform: device.platform,
      driver,
      orchestra,
      dispose: async () => {
        await driver.disconnect().catch(() => {});
      },
    });
    return device;
  }

  private diffAndEmit(
    previous: readonly DeviceDescriptor[],
    next: readonly DeviceDescriptor[],
  ): void {
    const prevMap = new Map(previous.map((d) => [d.id, d] as const));
    const nextMap = new Map(next.map((d) => [d.id, d] as const));
    for (const [id, device] of nextMap) {
      const before = prevMap.get(id);
      if (!before) {
        this.events.emit({
          event: "deviceConnected",
          payload: device,
        });
      } else if (before.state !== device.state) {
        this.events.emit({
          event: "deviceStateChanged",
          payload: device,
        });
      }
    }
    for (const [id, device] of prevMap) {
      if (!nextMap.has(id)) {
        this.events.emit({
          event: "deviceDisconnected",
          payload: device,
        });
      }
    }
  }
}
