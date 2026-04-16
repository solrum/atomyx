import { z } from "zod";
import { defineTool } from "../tool-definition.js";
import { discoverDevices } from "../device-discovery.js";

const ListDevicesArgs = z
  .object({
    platform: z
      .union([z.literal("ios"), z.literal("android"), z.literal("all")])
      .optional()
      .describe("Filter by platform. Default 'all'."),
  })
  .strict();

/**
 * `list_devices` — enumerate connected devices across iOS +
 * Android. Host-side helper (not a driver method — the point is
 * to list candidates BEFORE choosing which driver to
 * instantiate).
 *
 * Delegates to `discoverDevices` in `device-discovery.ts`, which
 * is the same enumeration code the `atomyx-mcp` binary uses for
 * its auto-select path when no explicit `--platform` / `--device`
 * flags are given. Sharing the discovery implementation between
 * the tool and the binary means "what the agent sees via
 * list_devices" is identical to "what the binary will auto-bind
 * to on startup".
 */
export const listDevicesTool = defineTool({
  name: "list_devices",
  description:
    "Enumerate connected devices across iOS + Android. Returns a flat list " +
    "with { platform, id, name, state, kind } entries. Use this BEFORE starting " +
    "the MCP server to pick the target device. Missing platform tools (no adb, " +
    "no xcrun) are silently skipped.",
  inputSchema: ListDevicesArgs,
  async execute(args, ctx) {
    const devices = await discoverDevices({
      platform: args.platform,
      onWarn: (msg, err) => {
        ctx.logger.debug(`list_devices: ${msg}`, { error: err.message });
      },
    });
    return {
      count: devices.length,
      devices,
      hint:
        devices.length === 0
          ? "No devices found. Check that adb / xcrun / libimobiledevice are " +
            "installed and a device/simulator is actually running."
          : undefined,
    };
  },
});
