import type { FeatureHandle, SidecarContext } from "../../infra/context.js";
import type { DeviceService } from "../device/index.js";
import type { InputService } from "../input/index.js";
import type { IosAgentService } from "../ios-agent/index.js";
import type { AndroidAgentService } from "../android-agent/index.js";
import { MirrorInputService } from "./mirror-input.service.js";
import { registerMirrorInputHandlers } from "./mirror-input.handlers.js";

export { MirrorInputService } from "./mirror-input.service.js";
export {
  DriverNotReadyError,
  StreamingTouchNotSupportedError,
} from "./mirror-input.errors.js";

export interface MirrorInputFeatureDeps {
  readonly deviceService: DeviceService;
  readonly inputService: InputService;
  readonly iosAgentService: IosAgentService;
  readonly androidAgentService: AndroidAgentService;
}

export function registerMirrorInputFeature(
  ctx: SidecarContext,
  deps: MirrorInputFeatureDeps,
): FeatureHandle {
  const service = new MirrorInputService({
    deviceService: deps.deviceService,
    inputService: deps.inputService,
    session: ctx.session,
    iosAgentService: deps.iosAgentService,
    androidAgentService: deps.androidAgentService,
  });
  registerMirrorInputHandlers(ctx.dispatcher, service);
  return {};
}
