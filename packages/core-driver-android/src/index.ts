/**
 * @atomyx/core-driver-android — Driver implementation for Android
 * devices backed by the Kotlin APK control server.
 *
 * Public surface:
 *
 *   - `AndroidDriver` — the `Driver` implementation. Pass to
 *     `Orchestra` as `{ driver: new AndroidDriver({serial}) }`.
 *
 *   - `normalizeAndroidTree` — the legacy → canonical tree
 *     translator. Exported so tests and Studio previews can
 *     normalize captured Android trees without running a live
 *     driver.
 *
 *   - `AdbError`, `HttpClientError` — typed errors consumers
 *     can catch to produce actionable diagnostics on connect
 *     failure.
 */

export * from "./android.driver.js";
export * from "./tree-normalizer.js";
export { AdbError, adbListDevices, type AdbDeviceEntry } from "./adb.js";
export { HttpClientError } from "./http-client.js";
