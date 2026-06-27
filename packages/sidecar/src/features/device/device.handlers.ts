import type { Dispatcher } from "../../infra/transport/dispatcher.js";
import { DispatcherError } from "../../infra/transport/dispatcher.js";
import type { DeviceService } from "./device.service.js";

export function registerDeviceHandlers(
  dispatcher: Dispatcher,
  service: DeviceService,
): void {
  dispatcher.register("listDevices", () => service.list());
  dispatcher.register("selectDevice", async (params) => {
    const id = (params as { id?: unknown })?.id;
    if (typeof id !== "string" || id.length === 0) {
      throw new DispatcherError("InvalidParams", "params.id must be a non-empty string");
    }
    return service.select(id);
  });
  dispatcher.register("deselectDevice", () => service.deselect());
}
