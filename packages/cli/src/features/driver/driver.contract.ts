import type { Driver } from "@atomyx/driver/driver";

export type DevicePlatform = "android" | "ios";

export interface DriverFactory {
  forPlatform(platform: DevicePlatform, deviceId?: string): Driver;
}
