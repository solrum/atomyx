import { adbListDevices } from "@atomyx/core-driver-android";

/**
 * `atomyx-driver list-devices` — enumerate currently-connected
 * devices that Atomyx can drive.
 *
 * Currently lists Android devices via `adb devices` only. iOS
 * device enumeration (via `idevice_id -l` for physical devices
 * and `xcrun simctl list devices` for simulators) is planned
 * but requires libimobiledevice / Xcode at runtime — pulling
 * those into the host CLI dependency surface is deferred until
 * the iOS device discovery flow is needed by a real consumer.
 *
 * Output format is human-readable text on stdout. Future
 * `--json` flag for scripting consumption is on the roadmap.
 */
export async function runListDevices(): Promise<void> {
  const out = process.stdout.write.bind(process.stdout);
  out("Atomyx — connected devices\n");
  out("\n");

  out("Android (via adb):\n");
  try {
    const devices = await adbListDevices();
    if (devices.length === 0) {
      out("  (none)\n");
    } else {
      for (const d of devices) {
        const state = d.state === "device" ? "ready" : d.state;
        const model = d.model ? ` ${d.model}` : "";
        out(`  ${d.serial}  [${state}]${model}\n`);
      }
    }
  } catch (err) {
    out(`  (adb not available: ${(err as Error).message})\n`);
  }

  out("\n");
  out("iOS (via idevice_id / xcrun): not yet implemented in CLI.\n");
  out("Use `idevice_id -l` (real devices) or `xcrun simctl list devices`\n");
  out("(simulators) directly until iOS device discovery lands.\n");
}
