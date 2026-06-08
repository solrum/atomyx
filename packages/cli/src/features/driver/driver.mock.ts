import type { Driver } from "@atomyx/driver/driver";
import { MockDriver } from "@atomyx/driver/testing";
import type { DevicePlatform, DriverFactory } from "./driver.contract.js";

export interface MockDriverFactory extends DriverFactory {
  readonly calls: DevicePlatform[];
}

export function createMockDriverFactory(seed?: {
  drivers?: Partial<Record<DevicePlatform, Driver>>;
}): MockDriverFactory {
  const calls: DevicePlatform[] = [];
  return {
    calls,
    forPlatform(platform) {
      calls.push(platform);
      return seed?.drivers?.[platform] ?? new MockDriver();
    },
  };
}
