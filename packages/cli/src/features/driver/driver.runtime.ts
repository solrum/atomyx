import { AndroidDriver } from "@atomyx/android-driver";
import { IosDriver } from "@atomyx/ios-driver";
import type { DriverFactory } from "./driver.contract.js";

// Default ADB serial assigned by Android Studio / emulator CLI
// when a single emulator boots. Used only when the caller omits
// `--device`; real runs should pass the id from `list-devices`.
const DEFAULT_ANDROID_EMULATOR_SERIAL = "emulator-5554";

export function createRuntimeDriverFactory(): DriverFactory {
  return {
    forPlatform(platform, deviceId) {
      switch (platform) {
        case "android":
          return new AndroidDriver({
            serial: deviceId ?? DEFAULT_ANDROID_EMULATOR_SERIAL,
          });
        case "ios":
          return new IosDriver({
            kind: "simulator",
            udid: deviceId ?? "",
          });
      }
    },
  };
}
