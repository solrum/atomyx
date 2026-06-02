import type { FeatureHandle, SidecarContext } from "../../infra/context.js";
import { registerMetaHandlers } from "./meta.handlers.js";

export { PROTOCOL_VERSION } from "./meta.handlers.js";

export function registerMetaFeature(ctx: SidecarContext): FeatureHandle {
  registerMetaHandlers(ctx.dispatcher);
  return {};
}
