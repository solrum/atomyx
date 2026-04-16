import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";

const ListAppsArgs = z.object({}).strict();

/**
 * `list_apps` — enumerate installed apps on the current device
 * via the driver. Android returns package names + labels from
 * `pm list packages`. iOS currently returns an empty list from
 * the Swift driver — the host composition layer populates via
 * xcrun (deferred to a future batch).
 */
export const listAppsTool = defineTool({
  name: "list_apps",
  description:
    "List apps installed on the current device. Returns bundle ids / " +
    "package names plus display labels. Call before launch_app when you " +
    "need to confirm the exact identifier.",
  inputSchema: ListAppsArgs,
  async execute(_args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    const apps = await orchestra.listApps();
    return {
      count: apps.length,
      apps,
      note:
        apps.length === 0
          ? "Driver returned no apps. iOS enumeration via xcrun is " +
            "deferred — use `xcrun simctl listapps` directly for now."
          : undefined,
    };
  },
});
