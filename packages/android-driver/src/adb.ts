import { execFile, type ExecFileException } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

/**
 * Minimal wrapper around the `adb` CLI for the lifecycle operations
 * this driver needs: port forwarding and device enumeration. Kept
 * tiny on purpose — anything beyond forwarding belongs in the
 * Kotlin APK or the platform test layer, not the host adapter.
 *
 * Error handling convention: the functions here throw a plain
 * `Error` with a descriptive message when `adb` is missing or
 * fails. Callers catch at the Driver boundary and convert into a
 * connect-time failure the consumer can report.
 */

export class AdbError extends Error {
  constructor(
    message: string,
    public readonly stderr?: string,
  ) {
    super(message);
    this.name = "AdbError";
  }
}

async function adb(args: string[]): Promise<{ stdout: string; stderr: string }> {
  try {
    const { stdout, stderr } = await execFileAsync("adb", args);
    return { stdout, stderr };
  } catch (err) {
    const e = err as ExecFileException & { stderr?: string };
    if (e.code === "ENOENT") {
      throw new AdbError(
        "adb not found on PATH. Install Android platform-tools " +
          "(brew install --cask android-platform-tools) and ensure " +
          "`adb` is reachable.",
      );
    }
    throw new AdbError(
      `adb ${args.join(" ")} failed: ${e.message}`,
      e.stderr ?? undefined,
    );
  }
}

/**
 * Spawn `adb -s <serial> forward tcp:<hostPort> tcp:<devicePort>`.
 * Idempotent on the adb side — re-running with the same ports is a
 * no-op, not an error.
 */
export async function adbForward(
  serial: string,
  hostPort: number,
  devicePort: number,
): Promise<void> {
  await adb(["-s", serial, "forward", `tcp:${hostPort}`, `tcp:${devicePort}`]);
}

/**
 * Remove a previously established forward. Best-effort — any
 * failure (e.g. forward already removed) is swallowed because
 * disconnect paths should not throw.
 */
export async function adbForwardRemove(serial: string, hostPort: number): Promise<void> {
  try {
    await adb(["-s", serial, "forward", "--remove", `tcp:${hostPort}`]);
  } catch {
    // best effort
  }
}

/**
 * Enumerate currently-connected Android devices (including
 * emulators). Parses the output of `adb devices -l`:
 *
 *   List of devices attached
 *   emulator-5554          device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64
 *   R3CT909XYZ             device product:a23xq model:SM_A235F
 *
 * Returns entries only for lines ending with `device` (skip
 * `offline`, `unauthorized`, `no device` rows).
 */
export interface AdbDeviceEntry {
  readonly serial: string;
  readonly state: "device" | "offline" | "unauthorized";
  readonly model?: string;
  readonly product?: string;
}

export async function adbListDevices(): Promise<AdbDeviceEntry[]> {
  const { stdout } = await adb(["devices", "-l"]);
  const lines = stdout.trim().split("\n").slice(1);
  const out: AdbDeviceEntry[] = [];
  for (const line of lines) {
    const parts = line.trim().split(/\s+/);
    if (parts.length < 2) continue;
    const [serial, state, ...extras] = parts;
    if (!serial || !state) continue;
    const entry: AdbDeviceEntry = {
      serial,
      state: state === "device" ? "device" : state === "offline" ? "offline" : "unauthorized",
      model: extras.find((e) => e.startsWith("model:"))?.slice("model:".length),
      product: extras.find((e) => e.startsWith("product:"))?.slice("product:".length),
    };
    out.push(entry);
  }
  return out;
}
