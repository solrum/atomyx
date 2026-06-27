/**
 * @atomyx/android-driver — Driver implementation for Android
 * devices backed by the Kotlin APK control server.
 *
 * Public surface:
 *
 *   - `AndroidDriver` — the `Driver` implementation. Pass to
 *     `Orchestra` as `{ driver: new AndroidDriver({serial}) }`.
 *
 *   - `normalizeAndroidTree` — translates the APK's wire shape
 *     into the canonical `TreeNodeWire`. Exported so tests and
 *     offline tooling can normalize captured Android trees
 *     without running a live driver.
 *
 *   - `AdbError`, `HttpClientError` — typed errors consumers
 *     can catch to produce actionable diagnostics on connect
 *     failure.
 */

export * from "./android.driver.js";
export * from "./clear/index.js";
export * from "./tree-normalizer.js";
export { AdbError, adbListDevices, type AdbDeviceEntry } from "./adb.js";
export { HttpClientError } from "./http-client.js";
export {
  AndroidAgentLauncher,
  AndroidAgentLauncherError,
  probeAndroidAgentHealth,
  type AndroidAgentLauncherOptions,
} from "./agent-launcher.js";
