import type { Device } from "../../../domain/features/runtime/index.js";
import type { MirrorTarget } from "../../../domain/features/mirror/index.js";

/**
 * Map a runtime `Device` to a `MirrorTarget`. The mirror layer
 * needs the finer "simulator vs real device" split so the
 * dispatcher picks the right adapter; runtime exposes that split
 * via `device.kind`.
 */
export function deviceToMirrorTarget(device: Device): MirrorTarget {
  if (device.platform === "android") {
    return {
      id: device.id,
      kind: "android",
      displayName: device.name,
    };
  }
  return {
    id: device.id,
    kind: device.kind === "simulator" ? "ios-simulator" : "ios-device",
    displayName: device.name,
  };
}
