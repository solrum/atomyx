import { spawn } from "node:child_process";

/**
 * Entry returned by {@link iosListDevices}. One entry per
 * reachable device (simulator OR physical).
 */
export interface IosDeviceEntry {
  readonly udid: string;
  readonly name: string;
  readonly kind: "simulator" | "device";
  /**
   * Simulator state — "Booted" / "Shutdown" / etc. — or "device"
   * for physical. Used by the host to decide whether the device
   * is addressable without booting it first.
   */
  readonly state: string;
  readonly runtime?: string;
}

interface SimctlDevice {
  readonly udid: string;
  readonly name: string;
  readonly state: string;
  readonly isAvailable: boolean;
}

interface SimctlListOutput {
  readonly devices: Record<string, readonly SimctlDevice[]>;
}

/**
 * Enumerate iOS simulators + physical devices visible on the
 * current host.
 *
 *   - Simulators are read from `xcrun simctl list devices --json`.
 *     Only "available" runtimes are returned. "Booted" comes first
 *     so host pickers default to a running sim.
 *
 *   - Physical devices are read from `idevice_id -l` (from
 *     libimobiledevice, typically `brew install libimobiledevice`).
 *     Missing tool is treated as "no physical devices" and
 *     silently ignored; simulators still work.
 */
export async function iosListDevices(): Promise<readonly IosDeviceEntry[]> {
  const [sims, phys] = await Promise.all([
    listSimulators().catch(() => [] as IosDeviceEntry[]),
    listPhysical().catch(() => [] as IosDeviceEntry[]),
  ]);
  return [...sortBootedFirst(sims), ...phys];
}

async function listSimulators(): Promise<IosDeviceEntry[]> {
  const json = await exec("xcrun", ["simctl", "list", "devices", "--json"]);
  const parsed = JSON.parse(json) as SimctlListOutput;
  const out: IosDeviceEntry[] = [];
  for (const [runtime, entries] of Object.entries(parsed.devices)) {
    const runtimeLabel = runtime.replace(/^com\.apple\.CoreSimulator\.SimRuntime\./, "");
    for (const e of entries) {
      if (!e.isAvailable) continue;
      out.push({
        udid: e.udid,
        name: e.name,
        kind: "simulator",
        state: e.state,
        runtime: runtimeLabel,
      });
    }
  }
  return out;
}

async function listPhysical(): Promise<IosDeviceEntry[]> {
  const raw = await exec("idevice_id", ["-l"]);
  const udids = raw
    .split("\n")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
  return Promise.all(
    udids.map(async (udid) => {
      const name = await exec("idevicename", ["-u", udid]).catch(() => udid);
      return {
        udid,
        name: name.trim() || udid,
        kind: "device" as const,
        state: "device",
      };
    }),
  );
}

function sortBootedFirst(entries: IosDeviceEntry[]): IosDeviceEntry[] {
  return [...entries].sort((a, b) => {
    if (a.state === b.state) return a.name.localeCompare(b.name);
    if (a.state === "Booted") return -1;
    if (b.state === "Booted") return 1;
    return a.name.localeCompare(b.name);
  });
}

function exec(cmd: string, args: readonly string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    const p = spawn(cmd, args, { stdio: ["ignore", "pipe", "pipe"] });
    let out = "";
    let err = "";
    p.stdout.on("data", (d: Buffer) => (out += d.toString("utf8")));
    p.stderr.on("data", (d: Buffer) => (err += d.toString("utf8")));
    p.on("error", reject);
    p.on("close", (code) => {
      if (code === 0) resolve(out);
      else reject(new Error(`${cmd} exited ${code}: ${err.trim()}`));
    });
  });
}
