import {
  DispatcherError,
  type Dispatcher,
} from "../../infra/transport/dispatcher.js";
import type { AndroidAgentService } from "./android-agent.service.js";

export function registerAndroidAgentHandlers(
  dispatcher: Dispatcher,
  service: AndroidAgentService,
): void {
  dispatcher.register("ensureAndroidAgent", async (params) => {
    const p = params as { serial?: unknown } | undefined;
    const serial = p?.serial;
    if (typeof serial !== "string" || serial.length === 0) {
      throw new DispatcherError(
        "InvalidParams",
        "params.serial must be a non-empty string",
      );
    }
    return service.ensure({ serial });
  });

  dispatcher.register("androidAgentStatus", async (params) => {
    const p = params as { serial?: unknown } | undefined;
    const serial = p?.serial;
    if (typeof serial !== "string" || serial.length === 0) {
      throw new DispatcherError(
        "InvalidParams",
        "params.serial must be a non-empty string",
      );
    }
    return service.status(serial);
  });
}
