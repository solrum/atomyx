import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";

const ListAppsArgs = z.object({}).strict();

/**
 * `list_apps` — enumerate installed apps on the current device via
 * the active driver. The driver abstracts the platform-specific
 * enumeration so callers receive a uniform list.
 */
export const listAppsTool = defineTool({
  name: "list_apps",
  description:
    "List apps installed on the current device. Returns app " +
    "identifiers plus display labels. Call before launch_app when you " +
    "need to confirm the exact identifier.",
  inputSchema: ListAppsArgs,
  async execute(_args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    const apps = await orchestra.listApps({ signal: ctx.signal });
    return {
      count: apps.length,
      apps,
      note:
        apps.length === 0
          ? "No apps returned. Device may have no installed apps, or " +
            "the host enumeration toolchain is not available."
          : undefined,
    };
  },
});
