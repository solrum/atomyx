import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { adbListDevices } from "@atomyx/android-driver";

const execFileAsync = promisify(execFile);

/**
 * `atomyx-driver list-devices` — enumerate currently-connected
 * devices that Atomyx can drive.
 *
 * Covers:
 * - Android devices via `adb devices -l`
 * - iOS simulators via `xcrun simctl list devices booted --json`
 * - iOS physical devices via `idevice_id -l` + `ideviceinfo`
 *
 * Missing host tools are silently skipped — a Linux CI without
 * Xcode still sees Android devices.
 */

interface DeviceEntry {
  readonly platform: "android" | "ios";
  readonly id: string;
  readonly name: string;
  readonly state: string;
  readonly kind: "device" | "emulator" | "simulator";
}

export interface ListDevicesOptions {
  readonly json?: boolean;
}

export async function runListDevices(
  opts: ListDevicesOptions = {},
): Promise<void> {
  const devices: DeviceEntry[] = [];
  const warnings: string[] = [];

  // ── Android ──────────────────────────────────────────────
  try {
    const adbDevices = await adbListDevices();
    for (const d of adbDevices) {
      devices.push({
        platform: "android",
        id: d.serial,
        name: d.model ?? d.serial,
        state: d.state === "device" ? "ready" : d.state,
        kind: d.serial.startsWith("emulator-") ? "emulator" : "device",
      });
    }
  } catch (err) {
    warnings.push(`adb not available: ${(err as Error).message}`);
  }

  // ── iOS simulators (booted) ──────────────────────────────
  try {
    const { stdout } = await execFileAsync("xcrun", [
      "simctl",
      "list",
      "devices",
      "booted",
      "--json",
    ]);
    const parsed = JSON.parse(stdout) as {
      devices: Record<
        string,
        Array<{ udid: string; name: string; state: string }>
      >;
    };
    for (const runtime of Object.values(parsed.devices ?? {})) {
      for (const sim of runtime) {
        devices.push({
          platform: "ios",
          id: sim.udid,
          name: sim.name,
          state: sim.state.toLowerCase(),
          kind: "simulator",
        });
      }
    }
  } catch (err) {
    warnings.push(`xcrun simctl not available: ${(err as Error).message}`);
  }

  // ── iOS physical devices ─────────────────────────────────
  try {
    const { stdout } = await execFileAsync("idevice_id", ["-l"]);
    const udids = stdout.trim().split("\n").filter(Boolean);
    for (const udid of udids) {
      let name = udid;
      try {
        const info = await execFileAsync("ideviceinfo", [
          "-u",
          udid,
          "-k",
          "DeviceName",
        ]);
        name = info.stdout.trim() || udid;
      } catch {
        // best effort — keep udid as name
      }
      devices.push({
        platform: "ios",
        id: udid,
        name,
        state: "connected",
        kind: "device",
      });
    }
  } catch {
    // idevice_id not installed — skip silently (common on
    // machines without libimobiledevice)
  }

  // ── Output ───────────────────────────────────────────────
  if (opts.json) {
    process.stdout.write(JSON.stringify({ devices, warnings }, null, 2) + "\n");
    return;
  }

  const out = process.stdout.write.bind(process.stdout);
  out("Atomyx — connected devices\n\n");

  const android = devices.filter((d) => d.platform === "android");
  out("Android (via adb):\n");
  if (android.length === 0) {
    out("  (none)\n");
  } else {
    for (const d of android) {
      const nameLabel = d.name !== d.id ? ` ${d.name}` : "";
      out(`  ${d.id}  [${d.state}]${nameLabel}\n`);
    }
  }

  out("\n");

  const iosSims = devices.filter(
    (d) => d.platform === "ios" && d.kind === "simulator",
  );
  out("iOS Simulators (booted):\n");
  if (iosSims.length === 0) {
    out("  (none)\n");
  } else {
    for (const d of iosSims) {
      out(`  ${d.id}  [${d.state}] ${d.name}\n`);
    }
  }

  out("\n");

  const iosDevices = devices.filter(
    (d) => d.platform === "ios" && d.kind === "device",
  );
  out("iOS Devices (USB):\n");
  if (iosDevices.length === 0) {
    out("  (none)\n");
  } else {
    for (const d of iosDevices) {
      out(`  ${d.id}  [${d.state}] ${d.name}\n`);
    }
  }

  if (warnings.length > 0) {
    out("\n");
    for (const w of warnings) {
      out(`  ⚠ ${w}\n`);
    }
  }
}
