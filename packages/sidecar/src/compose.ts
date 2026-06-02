import type { Readable, Writable } from "node:stream";
import { Dispatcher } from "./infra/transport/dispatcher.js";
import { StdioTransport } from "./infra/transport/stdio-transport.js";
import { EventBus } from "./infra/events/event-bus.js";
import { Session } from "./infra/session/session.js";
import type { FeatureHandle, SidecarContext } from "./infra/context.js";
import {
  registerDeviceFeature,
  DriverFactory,
  type DeviceFeatureOptions,
} from "./features/device/index.js";
import { registerAppFeature } from "./features/app/index.js";
import { registerScriptFeature } from "./features/script/index.js";
import { registerInspectionFeature } from "./features/inspection/index.js";
import { registerInputFeature } from "./features/input/index.js";
import {
  registerIosAgentFeature,
  type IosAgentFeatureHandle,
} from "./features/ios-agent/index.js";
import {
  registerAndroidAgentFeature,
  type AndroidAgentFeatureHandle,
} from "./features/android-agent/index.js";
import { registerMetaFeature } from "./features/meta/index.js";
import { registerMirrorInputFeature } from "./features/mirror-input/index.js";

export interface SidecarComposeOptions {
  readonly input: Readable;
  readonly output: Writable;
  readonly onError?: (err: unknown) => void;
  /**
   * Per-feature test hooks. Production callers leave this empty;
   * tests pass mock probes / factories here.
   */
  readonly device?: DeviceFeatureOptions;
}

export interface SidecarHandle {
  readonly dispatcher: Dispatcher;
  readonly events: EventBus;
  readonly session: Session;
  readonly transport: StdioTransport;
  start(): void;
  dispose(): Promise<void>;
}

/**
 * Composition root.
 *
 * Iterates a fixed FEATURES array — adding a sidecar capability
 * is one folder under features/ plus one line here. No per-
 * feature wiring leaks into this file because every feature owns
 * its own construction inside its index.ts.
 */
export function composeSidecar(opts: SidecarComposeOptions): SidecarHandle {
  const dispatcher = new Dispatcher();
  const events = new EventBus();
  const session = new Session();
  const ctx: SidecarContext = { dispatcher, events, session };

  // ios-agent must be registered before device so DriverFactory
  // can route simulator targets through the sim-hid helper when
  // ATOMYX_SIM_HID=1 is set. Without the iosAgentService the
  // factory has no path to start the helper and silently falls
  // back to XCUITest.
  const iosAgentHandle: IosAgentFeatureHandle = registerIosAgentFeature(ctx);
  const androidAgentHandle: AndroidAgentFeatureHandle =
    registerAndroidAgentFeature(ctx);
  const deviceHandle = registerDeviceFeature(ctx, {
    ...opts.device,
    factory:
      opts.device?.factory ??
      new DriverFactory({ iosAgentService: iosAgentHandle.service }),
  });
  const inputHandle = registerInputFeature(ctx);
  const handles: FeatureHandle[] = [
    registerMetaFeature(ctx),
    deviceHandle,
    registerAppFeature(ctx),
    registerScriptFeature(ctx),
    registerInspectionFeature(ctx),
    inputHandle,
    iosAgentHandle,
    androidAgentHandle,
    registerMirrorInputFeature(ctx, {
      deviceService: deviceHandle.service,
      inputService: inputHandle.service,
      iosAgentService: iosAgentHandle.service,
      androidAgentService: androidAgentHandle.service,
    }),
  ];

  const transport = new StdioTransport({
    input: opts.input,
    output: opts.output,
    dispatcher,
    onError: opts.onError,
  });
  events.subscribe((event) => transport.emit(event));

  return {
    dispatcher,
    events,
    session,
    transport,
    start() {
      transport.start();
    },
    async dispose() {
      for (const h of handles) {
        await h.dispose?.().catch(() => {});
      }
      await session.dispose();
    },
  };
}
