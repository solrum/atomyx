/**
 * @atomyx/ios-sim-driver — iOS Simulator direct-HID driver.
 *
 * Public surface:
 *
 *   - `createIosSimDriver(opts)` — factory. Always returns an
 *     `IosSimDriver` when `opts.hidPort` is provided. The decision
 *     to take the HID path vs. XCUITest is made by the caller
 *     (sidecar device factory) by gating on `ATOMYX_SIM_HID=1` and
 *     `isSimDirectSupported()`. When the helper start fails, the
 *     caller falls back to constructing an `IosDriver` directly.
 *
 *   - `isSimDirectSupported()` — system capability probe: arm64 host
 *     + Xcode version >= verified floor. Returns false on older
 *     Xcode or x86_64. Returning true means the system COULD run
 *     the HID path; explicit opt-in (`ATOMYX_SIM_HID=1`) is still
 *     required.
 *
 *   - `IosSimDriverOptions` — options type. Extends IosDriverOptions
 *     with `hidPort` so the factory knows which WS port the
 *     atomyx-sim-hid helper is listening on.
 *
 *   - `StreamingTouchCapable` / `isStreamingTouchCapable` —
 *     interface + type-guard for drivers that expose phase-by-phase
 *     touch dispatch (down / move / up). The sidecar checks this
 *     before routing streaming mirror input through the HID path.
 */

import type { Driver } from "@atomyx/driver";
import { IosSimDriver } from "./ios-sim-driver.impl.js";
import type { IosSimDriverOptions } from "./ios-sim-driver.impl.js";

export type { Driver } from "./ios-sim-driver.contract.js";
export type {
  IosSimDriverOptions,
  StreamingTouchCapable,
} from "./ios-sim-driver.impl.js";
export { isStreamingTouchCapable } from "./ios-sim-driver.impl.js";
export { isSimDirectSupported } from "./xcode-version.js";

/**
 * Constructs an `IosSimDriver` that routes touch through the
 * atomyx-sim-hid helper. `opts.hidPort` is required — the caller
 * (sidecar device factory) must start the helper and pass its
 * handshake port. When the helper cannot start, the caller is
 * responsible for falling back to `IosDriver` directly.
 */
export function createIosSimDriver(opts: IosSimDriverOptions): Driver {
  return new IosSimDriver(opts);
}
