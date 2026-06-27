import type { DeviceDescriptor, DeviceProbe } from "../features/device/index.js";

export class StaticProbe implements DeviceProbe {
  constructor(private readonly snapshots: DeviceDescriptor[][]) {}

  async scan(): Promise<readonly DeviceDescriptor[]> {
    const next = this.snapshots.shift() ?? [];
    return next;
  }
}
