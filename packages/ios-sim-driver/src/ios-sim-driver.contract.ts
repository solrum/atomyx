/**
 * Re-exports the `Driver` port from `@atomyx/driver` as the public
 * contract for `IosSimDriver`. No new methods are added — the Sim-
 * direct HID adapter implements the identical port; callers remain
 * unaware of which concrete driver they hold.
 */
export type { Driver } from "@atomyx/driver";
