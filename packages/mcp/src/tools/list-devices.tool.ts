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
 * `list_devices` — enumerate connected devices across all
 * supported platforms. Host-side helper (not a driver method — the
 * point is to list candidates BEFORE choosing which driver to
 * instantiate).
 *
 * Delegates to `discoverDevices` in `device-discovery.ts`, which
 * the `atomyx-mcp` binary also uses for its auto-select path when
 * no explicit `--platform` / `--device` flags are given. Sharing
 * the discovery implementation means "what the agent sees via
 * list_devices" is identical to "what the binary auto-binds to on
 * startup".
 */
export const listDevicesTool = defineTool({
  name: "list_devices",
  description:
    "Enumerate connected devices across all supported platforms. Returns " +
    "a flat list with { platform, id, name, state, kind } entries. Use " +
    "this BEFORE starting the MCP server to pick the target device. " +
    "Platforms whose toolchain isn't installed on the host are silently " +
    "skipped.",
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
          ? "No devices found. Check that the host platform toolchain is " +
            "installed and a device/simulator is actually running."
          : undefined,
    };
  },
});
