import type { FeatureHandle, SidecarContext } from "../../infra/context.js";
import { InputService } from "./input.service.js";
import { registerInputHandlers } from "./input.handlers.js";

export { InputService } from "./input.service.js";

export interface InputFeatureHandle extends FeatureHandle {
  readonly service: InputService;
}

export function registerInputFeature(ctx: SidecarContext): InputFeatureHandle {
  const service = new InputService({ session: ctx.session });
  registerInputHandlers(ctx.dispatcher, service);
  return { service };
}
