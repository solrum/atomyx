import type { FeatureHandle, SidecarContext } from "../../infra/context.js";
import { InspectionService } from "./inspection.service.js";
import { registerInspectionHandlers } from "./inspection.handlers.js";

export { InspectionService } from "./inspection.service.js";

export function registerInspectionFeature(ctx: SidecarContext): FeatureHandle {
  const service = new InspectionService({ session: ctx.session });
  registerInspectionHandlers(ctx.dispatcher, service);
  return {};
}
