import { z } from "zod";
import { defineTool } from "../tool-definition.js";

/**
 * `select_device` — bind the MCP session to a specific device.
 *
 * This is the runtime device-selection entry point. Agents
 * typically:
 *
 *   1. call `list_devices` to enumerate connected iOS + Android
 *      candidates
 *   2. call `select_device({platform, id, ...})` to bind one
 *   3. run the usual tool flow (launch_app, tap, get_ui_tree,
 *      ...) which goes through the bound device's Orchestra
 *   4. optionally call `select_device` again mid-session to
 *      switch to a different device — the previous driver is
 *      disconnected first so there's at most one active driver
 *      at a time
 *
 * Returns `{ok: true, active: {...}}` on success or
 * `{ok: false, reason}` on factory / connect failure. The
 * session stays IDLE (no active device) on failure so the agent
 * can retry a different selection without a restart.
 */

const SelectDeviceArgs = z
  .object({
    platform: z
      .union([z.literal("ios"), z.literal("android")])
      .describe("Which driver to bind. Match `platform` from list_devices."),
    id: z
      .string()
      .min(1)
      .describe("Device id — Android serial (adb) or iOS UDID."),
    kind: z
      .union([z.literal("simulator"), z.literal("device")])
      .optional()
      .describe(
        "iOS only. 'simulator' uses direct loopback, 'device' spawns " +
          "iproxy over USB. Default 'simulator' on iOS. Ignored on Android.",
      ),
    port: z
      .number()
      .int()
      .positive()
      .optional()
      .describe("iOS only. Override the Swift driver TCP port (default 22087)."),
  })
  .strict();

export const selectDeviceTool = defineTool({
  name: "select_device",
  description:
    "Bind the MCP session to a device so subsequent tools (launch_app, " +
    "tap, get_ui_tree, etc.) operate on it. Enumerate candidates with " +
    "list_devices first. Calling this again mid-session switches to a " +
    "different device — the previous driver is disconnected cleanly.",
  inputSchema: SelectDeviceArgs,
  async execute(args, ctx) {
    try {
      const active = await ctx.session.select({
        platform: args.platform,
        id: args.id,
        kind: args.kind,
        port: args.port,
      });
      return {
        ok: true,
        active: {
          platform: active.platform,
          id: active.id,
          kind: active.kind,
          connectedAt: active.connectedAt,
        },
      };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return { ok: false, reason: message };
    }
  },
});

const DisconnectDeviceArgs = z.object({}).strict();

export const disconnectDeviceTool = defineTool({
  name: "disconnect_device",
  description:
    "Disconnect the currently-active device and leave the session idle. " +
    "Subsequent tool calls that need a device will fail until " +
    "select_device is called again. Returns ok:true even when no device " +
    "was bound (idempotent).",
  inputSchema: DisconnectDeviceArgs,
  async execute(_args, ctx) {
    const wasActive = ctx.session.current();
    await ctx.session.disconnect();
    return {
      ok: true,
      wasActive: wasActive
        ? { platform: wasActive.platform, id: wasActive.id }
        : null,
    };
  },
});
