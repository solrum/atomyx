import type { DeviceInfo } from "../adapters/device-controller.port.js";
import { connectDevice, listAllDevices } from "../adapters/device-router.js";
import type { AdetContext } from "../runtime/adet-context.js";
import type { JsonSchema } from "../types.js";
import { Tool } from "./core/tool.js";
import { preflight, type PreflightResult } from "./preflight.js";

// ── list_devices ──────────────────────────────────────────────────────

export class ListDevicesTool extends Tool<{
  args: Record<string, never>;
  result: { devices: DeviceInfo[]; selected: string | null };
}> {
  readonly name = "list_devices";
  readonly description = "List all connected Android (ADB) and iOS devices.";
  readonly schema: JsonSchema = { type: "object", properties: {} };

  async execute(_args: Record<string, never>, ctx: AdetContext) {
    const devices = await listAllDevices();
    return { devices, selected: ctx.controller?.deviceId ?? null };
  }
}

// ── select_device ─────────────────────────────────────────────────────

export interface SelectDeviceArgs {
  deviceId: string;
}

export type SelectDeviceResult =
  | { ok: true; selected: string; platform: "android" | "ios" }
  | { ok: false; selected: string; platform: "android" | "ios"; preflight: PreflightResult };

export class SelectDeviceTool extends Tool<{
  args: SelectDeviceArgs;
  result: SelectDeviceResult;
}> {
  readonly name = "select_device";
  readonly description =
    "Select a device for subsequent actions. Establishes the underlying session: " +
    "ADB port-forward + agent handshake (Android) or XCTest bridge (iOS).";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["deviceId"],
    properties: {
      deviceId: { type: "string", description: "Device ID/serial from list_devices" },
    },
  };

  async execute(args: SelectDeviceArgs, ctx: AdetContext): Promise<SelectDeviceResult> {
    if (ctx.controller) {
      await ctx.controller.dispose();
    }
    ctx.controller = await connectDevice(args.deviceId);

    const check = await preflight(ctx.controller);
    if (!check.ok) {
      return {
        ok: false,
        selected: ctx.controller.deviceId,
        platform: ctx.controller.platform,
        preflight: check,
      };
    }
    return {
      ok: true,
      selected: ctx.controller.deviceId,
      platform: ctx.controller.platform,
    };
  }
}
