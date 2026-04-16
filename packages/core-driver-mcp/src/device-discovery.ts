import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

/**
 * Host-side device discovery — enumerates every iOS + Android
 * device the local machine can reach. Shared between:
 *
 *   - `list_devices` tool (exposes the list to agents over MCP)
 *   - `atomyx-mcp` binary auto-detect (picks a default device
 *     when the user doesn't pass --platform / --device)
 *
 * Missing CLI tools are silently skipped. Users on a Linux CI
 * box without Xcode see an empty iOS list but still get Android
 * devices; Mac contributors with Xcode + libimobiledevice see
 * everything. No hard dependency on any host command.
 *
 * The output is intentionally a plain data shape with no
 * framework types — both consumers want a flat list they can
 * filter and match on.
 */
export interface DiscoveredDevice {
  readonly platform: "ios" | "android";
  readonly id: string;
  readonly name: string;
  readonly state: string;
  readonly kind: "simulator" | "emulator" | "device";
}

export interface DiscoverOptions {
  /**
   * Filter the discovery to a single platform. Default: both.
   * Saves an unnecessary `xcrun` invocation on Linux CI that
   * only cares about Android.
   */
  readonly platform?: "ios" | "android" | "all";
  /**
   * Optional logger hook for "adb not installed" / "xcrun
   * missing" warnings. Defaults to no-op so callers without a
   * logger get a clean surface.
   */
  readonly onWarn?: (msg: string, err: Error) => void;
}

/**
 * Enumerate connected devices. Never throws — tool failures are
 * reported via `onWarn` and the corresponding platform yields
 * zero entries.
 */
export async function discoverDevices(
  opts: DiscoverOptions = {},
): Promise<DiscoveredDevice[]> {
  const platform = opts.platform ?? "all";
  const onWarn = opts.onWarn ?? (() => {});
  const devices: DiscoveredDevice[] = [];

  if (platform === "android" || platform === "all") {
    try {
      const { stdout } = await execFileAsync("adb", ["devices", "-l"]);
      for (const line of stdout.trim().split("\n").slice(1)) {
        const parts = line.trim().split(/\s+/);
        if (parts.length < 2) continue;
        const [id, state, ...extras] = parts;
        if (!id || !state) continue;
        const model = extras
          .find((e) => e.startsWith("model:"))
          ?.slice("model:".length);
        devices.push({
          platform: "android",
          id,
          name: model ?? id,
          state,
          kind: id.startsWith("emulator-") ? "emulator" : "device",
        });
      }
    } catch (err) {
      onWarn("adb unavailable", err as Error);
    }
  }

  if (platform === "ios" || platform === "all") {
    // Simulators (booted)
    try {
      const { stdout } = await execFileAsync("xcrun", [
        "simctl",
        "list",
        "devices",
        "booted",
        "--json",
      ]);
      const parsed = JSON.parse(stdout) as {
        devices: Record<string, Array<{ udid: string; name: string; state: string }>>;
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
      onWarn("xcrun unavailable", err as Error);
    }

    // Physical devices via libimobiledevice
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
    } catch (err) {
      onWarn("idevice_id unavailable", err as Error);
    }
  }

  return devices;
}

/**
 * Thrown by `autoSelectDevice` when it cannot pick a single
 * unambiguous device. The message is actionable — tells the
 * caller exactly what to do to disambiguate.
 */
export class AutoSelectError extends Error {
  constructor(
    message: string,
    public readonly candidates: readonly DiscoveredDevice[],
  ) {
    super(message);
    this.name = "AutoSelectError";
  }
}

/**
 * Pick a single device for `atomyx-mcp` to bind to when the user
 * doesn't pass --platform / --device explicitly. The rule is:
 *
 *   - 1 device total → use it
 *   - 0 devices → throw with a "plug a device in" message
 *   - >1 devices → throw listing candidates + the exact flags
 *     to disambiguate
 *
 * Intentionally simple. An agent that wants to drive a specific
 * device can always pass --platform + --device; this is just the
 * "one phone plugged in, Just Work" path.
 */
export async function autoSelectDevice(): Promise<DiscoveredDevice> {
  const devices = await discoverDevices();
  if (devices.length === 0) {
    throw new AutoSelectError(
      "no devices found. Plug in an Android device (USB debugging on) " +
        "or boot an iOS simulator, then retry. Check adb / xcrun are " +
        "installed if you expected one to appear.",
      [],
    );
  }
  if (devices.length === 1) {
    return devices[0]!;
  }
  const lines = devices.map(
    (d, i) => `  [${i + 1}] ${d.platform} ${d.kind} ${d.id} (${d.name})`,
  );
  throw new AutoSelectError(
    `${devices.length} devices found, cannot auto-select. Pass ` +
      `--platform and --device explicitly to disambiguate:\n${lines.join("\n")}\n\n` +
      `Example: atomyx-mcp --platform ${devices[0]!.platform} --device ${devices[0]!.id}`,
    devices,
  );
}
