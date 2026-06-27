/**
 * @atomyx/ios-driver — Driver implementation for iOS devices
 * backed by the Swift XCUITest runner.
 *
 * Public surface:
 *
 *   - `IosDriver` — the `Driver` implementation. Pass to
 *     `Orchestra` as `{ driver: new IosDriver({kind, udid}) }`.
 *
 *   - `normalizeIosTree` + `iosElementTypeToRole` — translate
 *     an iOS snapshot into the canonical `TreeNodeWire` shape.
 *     Exported so tests and offline tooling can normalize
 *     captured iOS trees without a live driver.
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
export * from "./clear/index.js";
export * from "./tree-normalizer.js";
export { iosListDevices, type IosDeviceEntry } from "./list-devices.js";
export { TcpClient, TcpClientError, type TcpClientOptions } from "./tcp-client.js";
export { Iproxy, IproxyError, type IproxyOptions } from "./iproxy.js";
export {
  XctestLauncher,
  XctestLauncherError,
  type XctestLauncherOptions,
  canConnect,
  probeDriverPing,
} from "./xctest-launcher.js";
