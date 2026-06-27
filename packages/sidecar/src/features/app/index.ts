import type { FeatureHandle, SidecarContext } from "../../infra/context.js";
import { AppService } from "./app.service.js";
import { registerAppHandlers } from "./app.handlers.js";

export * from "./app.types.js";
export { AppService } from "./app.service.js";

export function registerAppFeature(ctx: SidecarContext): FeatureHandle {
  const service = new AppService({ session: ctx.session });
  registerAppHandlers(ctx.dispatcher, service);
  return {};
}
