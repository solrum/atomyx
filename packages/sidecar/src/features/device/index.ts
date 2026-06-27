import type { FeatureHandle, SidecarContext } from "../../infra/context.js";
import {
  AndroidAdbProbe,
  CompositeDeviceProbe,
  IosProbe,
  type DeviceProbe,
} from "./device.probe.js";
import { DriverFactory } from "./device.driver-factory.js";
import { DeviceService } from "./device.service.js";
import { registerDeviceHandlers } from "./device.handlers.js";

export * from "./device.types.js";
export { DeviceService } from "./device.service.js";
export {
  AndroidAdbProbe,
  CompositeDeviceProbe,
  IosProbe,
  type DeviceProbe,
} from "./device.probe.js";
export { DriverFactory } from "./device.driver-factory.js";

export interface DeviceFeatureOptions {
  readonly probe?: DeviceProbe;
  readonly factory?: DriverFactory;
}

export interface DeviceFeatureHandle extends FeatureHandle {
  readonly service: DeviceService;
}

export function registerDeviceFeature(
  ctx: SidecarContext,
  options: DeviceFeatureOptions = {},
): DeviceFeatureHandle {
  const probe =
    options.probe ??
    new CompositeDeviceProbe([new AndroidAdbProbe(), new IosProbe()]);
  const factory = options.factory ?? new DriverFactory();
  const service = new DeviceService({
    probe,
    factory,
    session: ctx.session,
    events: ctx.events,
  });
  registerDeviceHandlers(ctx.dispatcher, service);
  return { service };
}
