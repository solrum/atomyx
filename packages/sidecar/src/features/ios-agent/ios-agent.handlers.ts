import {
  DispatcherError,
  type Dispatcher,
} from "../../infra/transport/dispatcher.js";
import type { IosAgentService } from "./ios-agent.service.js";

export function registerIosAgentHandlers(
  dispatcher: Dispatcher,
  service: IosAgentService,
): void {
  dispatcher.register("ensureIosAgent", async (params) => {
    const p = params as { udid?: unknown; kind?: unknown } | undefined;
    const udid = p?.udid;
    const kind = p?.kind;
    if (typeof udid !== "string" || udid.length === 0) {
      throw new DispatcherError(
        "InvalidParams",
        "params.udid must be a non-empty string",
      );
    }
    if (kind !== "simulator" && kind !== "device") {
      throw new DispatcherError(
        "InvalidParams",
        'params.kind must be "simulator" or "device"',
      );
    }
    return service.ensure({ udid, kind });
  });

  dispatcher.register("iosAgentStatus", async (params) => {
    const p = params as { udid?: unknown } | undefined;
    const udid = p?.udid;
    if (typeof udid !== "string" || udid.length === 0) {
      throw new DispatcherError(
        "InvalidParams",
        "params.udid must be a non-empty string",
      );
    }
    return service.status(udid);
  });

  dispatcher.register("iosSimHidStatus", async (params) => {
    const p = params as { udid?: unknown } | undefined;
    const udid = p?.udid;
    if (typeof udid !== "string" || udid.length === 0) {
      throw new DispatcherError(
        "InvalidParams",
        "params.udid must be a non-empty string",
      );
    }
    return service.simHidStatus(udid);
  });
}
