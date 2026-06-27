import type { Dispatcher } from "../../infra/transport/dispatcher.js";
import { DispatcherError } from "../../infra/transport/dispatcher.js";
import type { AppService } from "./app.service.js";

export function registerAppHandlers(
  dispatcher: Dispatcher,
  service: AppService,
): void {
  dispatcher.register("listApps", () => service.list());
  dispatcher.register("launchApp", async (params) => {
    const p = params as {
      readonly bundleId?: unknown;
      readonly args?: unknown;
      readonly env?: unknown;
      readonly noReset?: unknown;
    } | undefined;
    if (typeof p?.bundleId !== "string" || p.bundleId.length === 0) {
      throw new DispatcherError(
        "InvalidParams",
        "params.bundleId must be a non-empty string",
      );
    }
    const args = Array.isArray(p.args) ? (p.args as string[]) : undefined;
    const env =
      p.env && typeof p.env === "object"
        ? (p.env as Record<string, string>)
        : undefined;
    const noReset = typeof p.noReset === "boolean" ? p.noReset : undefined;
    await service.launch(p.bundleId, { args, env, noReset });
    return null;
  });
  dispatcher.register("currentForeground", () => service.currentForeground());
}
