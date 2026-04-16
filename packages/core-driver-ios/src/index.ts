/**
 * @atomyx/core-driver-ios — Driver implementation for iOS devices
 * backed by the Swift XCUITest runner.
 *
 * Public surface:
 *
 *   - `IosDriver` — the `Driver` implementation. Pass to
 *     `Orchestra` as `{ driver: new IosDriver({kind, udid}) }`.
 *
 *   - `normalizeIosTree` + `iosElementTypeToRole` — the legacy
 *     iOS snapshot → canonical tree translation functions.
 *     Exported so tests, Studio previews, and any Swift driver
 *     migration tooling can reuse them without a live driver.
 *
 *   - `TcpClient` + `Iproxy` — lower-level transport primitives.
 *     Exported for advanced consumers (e.g. diagnostics CLIs or
 *     multi-connection scenarios). Most callers never touch these
 *     directly.
 *
 *   - Typed errors — `TcpClientError`, `IproxyError`. Consumers
 *     catch these on `connect()` to produce actionable diagnostics
 *     (missing libimobiledevice, sim-driver collision, device
 *     not paired, etc.).
 */

export * from "./ios.driver.js";
export * from "./tree-normalizer.js";
export { TcpClient, TcpClientError, type TcpClientOptions } from "./tcp-client.js";
export { Iproxy, IproxyError, type IproxyOptions } from "./iproxy.js";
export {
  XctestLauncher,
  XctestLauncherError,
  type XctestLauncherOptions,
  canConnect,
  probeDriverPing,
} from "./xctest-launcher.js";
