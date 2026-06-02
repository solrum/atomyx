import type { Driver } from "@atomyx/driver";
import { Orchestra, SystemClock } from "@atomyx/driver";
import { MockDriver } from "@atomyx/driver/testing";
import type { DeviceDescriptor } from "../features/device/index.js";

/**
 * Replaces DriverFactory in tests — returns a MockDriver wrapped
 * in a real Orchestra. No ADB / iproxy / network access.
 */
export class MockDriverFactory {
  async build(_device: DeviceDescriptor): Promise<{
    readonly driver: Driver;
    readonly orchestra: Orchestra;
  }> {
    const driver = new MockDriver();
    await driver.connect();
    const orchestra = new Orchestra({ driver, clock: new SystemClock() });
    return { driver, orchestra };
  }
}
