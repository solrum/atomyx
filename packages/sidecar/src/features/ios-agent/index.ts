import type { FeatureHandle, SidecarContext } from "../../infra/context.js";
import { IosAgentService } from "./ios-agent.service.js";
import { registerIosAgentHandlers } from "./ios-agent.handlers.js";

export * from "./ios-agent.types.js";
export { IosAgentService } from "./ios-agent.service.js";

export interface IosAgentFeatureHandle extends FeatureHandle {
  readonly service: IosAgentService;
}

export function registerIosAgentFeature(
  ctx: SidecarContext,
): IosAgentFeatureHandle {
  const service = new IosAgentService({ events: ctx.events });
  registerIosAgentHandlers(ctx.dispatcher, service);
  return {
    service,
    dispose: async () => {
      await service.dispose();
    },
  };
}
