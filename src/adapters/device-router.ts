import { spawn } from "node:child_process";
import { AgentDirectController } from "./agent-direct.adapter.js";
import { IosXctestController } from "./ios-xctest.adapter.js";
import type { DeviceController, DeviceInfo } from "./device-controller.port.js";

const ANDROID_FORWARD_PORT_BASE = 18760;
let nextPort = ANDROID_FORWARD_PORT_BASE;

async function adb(args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    const proc = spawn("adb", args);
    let stdout = "";
    let stderr = "";
    proc.stdout.on("data", (c) => (stdout += c.toString()));
    proc.stderr.on("data", (c) => (stderr += c.toString()));
    proc.on("close", (code) => (code === 0 ? resolve(stdout) : reject(new Error(stderr))));
  });
}

async function xcrun(args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    const proc = spawn("xcrun", args);
    let stdout = "";
    let stderr = "";
    proc.stdout.on("data", (c) => (stdout += c.toString()));
    proc.stderr.on("data", (c) => (stderr += c.toString()));
    proc.on("close", (code) => (code === 0 ? resolve(stdout) : reject(new Error(stderr || `xcrun exit ${code}`))));
  });
}

export async function listAllDevices(): Promise<DeviceInfo[]> {
  const devices: DeviceInfo[] = [];

  // Android via adb
  try {
    const out = await adb(["devices"]);
    for (const line of out.split("\n").slice(1)) {
      const [serial, state] = line.trim().split(/\s+/);
      if (serial && state === "device") {
        devices.push({ id: serial, serial, platform: "android", state });
      }
    }
  } catch {
    // adb not available
  }

  // iOS simulators via simctl
  try {
    const out = await xcrun(["simctl", "list", "-j", "devices", "booted"]);
    const parsed = JSON.parse(out) as { devices: Record<string, Array<{ udid: string; name: string; state: string }>> };
    for (const runtimeDevices of Object.values(parsed.devices)) {
      for (const d of runtimeDevices) {
        if (d.state === "Booted") {
          devices.push({
            id: d.udid,
            serial: d.udid,
            platform: "ios",
            kind: "sim",
            model: d.name,
            state: d.state,
          });
        }
      }
    }
  } catch {
    // xcrun / simctl not available
  }

  // iOS physical devices via libimobiledevice (`idevice_id -l` lists
  // UDIDs of connected devices; `ideviceinfo -u <UDID> -k DeviceName`
  // gives the model name). Silently skip if libimobiledevice isn't
  // installed — real-device support is optional, simulator flow is
  // the primary path.
  try {
    const udidOut = await spawnCapture("idevice_id", ["-l"]);
    const udids = udidOut.split("\n").map((s) => s.trim()).filter((s) => s.length > 0);
    for (const udid of udids) {
      let model: string | undefined;
      try {
        model = (await spawnCapture("ideviceinfo", ["-u", udid, "-k", "DeviceName"])).trim() || undefined;
      } catch {
        // ideviceinfo may fail if device isn't trusted — still surface the udid
      }
      devices.push({
        id: udid,
        serial: udid,
        platform: "ios",
        kind: "device",
        model,
        state: "device",
      });
    }
  } catch {
    // libimobiledevice not installed — simulator enumeration still worked
  }

  return devices;
}

async function spawnCapture(cmd: string, args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    const proc = spawn(cmd, args);
    let stdout = "";
    let stderr = "";
    proc.stdout.on("data", (c) => (stdout += c.toString()));
    proc.stderr.on("data", (c) => (stderr += c.toString()));
    proc.on("error", reject);
    proc.on("close", (code) =>
      code === 0 ? resolve(stdout) : reject(new Error(stderr || `${cmd} exit ${code}`)),
    );
  });
}

export async function connectDevice(deviceId: string): Promise<DeviceController> {
  const all = await listAllDevices();
  const info = all.find((d) => d.id === deviceId || d.serial === deviceId);
  if (!info) throw new Error(`Device not found: ${deviceId}`);

  if (info.platform === "android") {
    const port = nextPort++;
    return AgentDirectController.connect(info.serial, port);
  }
  return IosXctestController.connect(info.serial, undefined, info.kind ?? "sim");
}
