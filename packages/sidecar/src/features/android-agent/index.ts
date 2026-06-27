import type { FeatureHandle, SidecarContext } from "../../infra/context.js";
import { AndroidAgentService } from "./android-agent.service.js";
import { registerAndroidAgentHandlers } from "./android-agent.handlers.js";

export * from "./android-agent.types.js";
export { AndroidAgentService } from "./android-agent.service.js";

export interface AndroidAgentFeatureHandle extends FeatureHandle {
  readonly service: AndroidAgentService;
}

export function registerAndroidAgentFeature(
  ctx: SidecarContext,
): AndroidAgentFeatureHandle {
  const service = new AndroidAgentService({ events: ctx.events });
  registerAndroidAgentHandlers(ctx.dispatcher, service);
  return {
    service,
    dispose: async () => {
      await service.dispose();
    },
  };
}
