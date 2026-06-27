import type { FeatureHandle, SidecarContext } from "../../infra/context.js";
import { ScriptRunnerService } from "./script.service.js";
import { registerScriptHandlers } from "./script.handlers.js";

export { ScriptRunnerService } from "./script.service.js";

export function registerScriptFeature(ctx: SidecarContext): FeatureHandle {
  const service = new ScriptRunnerService({
    session: ctx.session,
    events: ctx.events,
  });
  registerScriptHandlers(ctx.dispatcher, service);
  return {};
}
