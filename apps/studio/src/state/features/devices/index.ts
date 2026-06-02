import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type { StudioRuntime } from "../../../domain/features/runtime/index.js";
import type { DevicesApi, DevicesSnapshot, Device } from "./devices.contract.js";
import { createZustandDevices } from "./devices.zustand.js";

export type { DevicesApi, DevicesSnapshot, Device };
export { deviceToMirrorTarget } from "./mirror-target.js";

export const DEVICES_KEY = "devices";

/**
 * Composition-root factory. Called once from `main.tsx`; the
 * resulting instance is put into the FeatureRegistry.
 */
export function createDevices(deps: { runtime: StudioRuntime }): DevicesApi {
  return createZustandDevices(deps);
}

/** React hook — subscribes the component to state changes. */
export function useDevices(): DevicesSnapshot & {
  readonly refresh: DevicesApi["refresh"];
  readonly select: DevicesApi["select"];
} {
  const api = getFeature<DevicesApi>(DEVICES_KEY);
  const snapshot = useSyncExternalStore(
    api.subscribe,
    api.getSnapshot,
    api.getSnapshot,
  );
  return {
    ...snapshot,
    refresh: api.refresh,
    select: api.select,
  };
}
