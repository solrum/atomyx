import { adbListDevices } from "@atomyx/android-driver";
import { iosListDevices } from "@atomyx/ios-driver";
import type { DeviceDescriptor } from "./device.types.js";

/**
 * Single-source scanner for devices reachable from this host.
 *
 * Platform probes live behind a uniform interface so
 * DeviceService can stay ignorant of which toolchains are
 * available. Add a new platform = add a new probe, no change to
 * the consumer.
 */
export interface DeviceProbe {
  scan(): Promise<readonly DeviceDescriptor[]>;
}

export class AndroidAdbProbe implements DeviceProbe {
  async scan(): Promise<readonly DeviceDescriptor[]> {
    try {
      const entries = await adbListDevices();
      return entries.map((e) => ({
        id: e.serial,
        platform: "android" as const,
        name: e.serial,
        kind: e.serial.startsWith("emulator-") ? "emulator" : "device",
        state: e.state === "device" ? "online" : e.state === "unauthorized" ? "unauthorized" : "offline",
      }));
    } catch {
      // `adb devices` may fail when adb is missing — treat as empty.
      return [];
    }
  }
}

export class IosProbe implements DeviceProbe {
  async scan(): Promise<readonly DeviceDescriptor[]> {
    try {
      const entries = await iosListDevices();
      // Mirror Android's adb: only report devices the host can
      // actually talk to right now. A shutdown simulator is
      // installed but not reachable — listing it alongside booted
      // sims makes the picker useless (20+ entries of inert
      // choices).
      return entries
        .filter((e) => isReachable(e.state))
        .map((e) => ({
          id: e.udid,
          platform: "ios" as const,
          name: e.runtime ? `${e.name} · ${e.runtime}` : e.name,
          kind: e.kind,
          state: "online" as const,
        }));
    } catch {
      // Missing xcrun (non-macOS) or simctl failure — empty list
      // so the Android side still works.
      return [];
    }
  }
}

function isReachable(state: string): boolean {
  // simctl "Booted" = running simulator; idevice_id "device" =
  // paired physical. Everything else (Shutdown, Booting,
  // ShuttingDown) is skipped.
  return state === "Booted" || state === "device";
}

/**
 * Aggregates several probes; a failure on any single probe must
 * NOT hide devices visible to the others.
 */
export class CompositeDeviceProbe implements DeviceProbe {
  constructor(private readonly probes: readonly DeviceProbe[]) {}

  async scan(): Promise<readonly DeviceDescriptor[]> {
    const results = await Promise.allSettled(this.probes.map((p) => p.scan()));
    const flat: DeviceDescriptor[] = [];
    for (const r of results) {
      if (r.status === "fulfilled") flat.push(...r.value);
    }
    return flat;
  }
}
