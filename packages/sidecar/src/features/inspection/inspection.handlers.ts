import type { Dispatcher } from "../../infra/transport/dispatcher.js";
import type { InspectionService } from "./inspection.service.js";

export function registerInspectionHandlers(
  dispatcher: Dispatcher,
  service: InspectionService,
): void {
  dispatcher.register("getUiTree", async (params) => {
    const fresh = (params as { fresh?: unknown })?.fresh === true;
    return service.getUiTree({ fresh });
  });
  dispatcher.register("screenshot", async () => {
    const bytes = await service.screenshot();
    return { bytesBase64: Buffer.from(bytes).toString("base64") };
  });
  dispatcher.register("invalidateUiCache", () => {
    service.invalidate();
    return null;
  });
}
