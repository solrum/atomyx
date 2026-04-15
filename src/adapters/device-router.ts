import { spawn } from "node:child_process";
import { AgentDirectController } from "./agent-direct.adapter.js";
import { IosXctestController } from "./ios-xctest.adapter.js";
import type { DeviceController, DeviceInfo } from "./device-controller.port.js";

const ENGINE_URL = process.env.SYNAPSE_ENGINE_URL ?? "http://localhost:3000";
const FORWARD_PORT_BASE = 18760;

let nextPort = FORWARD_PORT_BASE;

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

export async function listAllDevices(): Promise<DeviceInfo[]> {
  const devices: DeviceInfo[] = [];

  // Android via ADB
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

  // iOS via engine (Mac host running Appium)
  try {
    const res = await fetch(`${ENGINE_URL}/api/devices/ios`);
    if (res.ok) {
      const ios = (await res.json()) as Array<{ id: string; serial: string; model?: string; state: string }>;
      for (const d of ios) devices.push({ ...d, platform: "ios" });
    }
  } catch {
    // engine not running or no iOS support
  }

  return devices;
}

export async function connectDevice(deviceId: string): Promise<DeviceController> {
  const all = await listAllDevices();
  const info = all.find((d) => d.id === deviceId || d.serial === deviceId);
  if (!info) throw new Error(`Device not found: ${deviceId}`);

  if (info.platform === "android") {
    const port = nextPort++;
    return AgentDirectController.connect(info.serial, port);
  }
  return IosXctestController.connect(info.serial);
}
