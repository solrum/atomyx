import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type { StudioRuntime } from "../../../domain/features/runtime/index.js";
import type {
  RuntimeStatusApi,
  RuntimeStatusSnapshot,
  RuntimeConnectivity,
} from "./runtime-status.contract.js";
import { createZustandRuntimeStatus } from "./runtime-status.zustand.js";

export type { RuntimeStatusApi, RuntimeStatusSnapshot, RuntimeConnectivity };

export const RUNTIME_STATUS_KEY = "runtime-status";

export function createRuntimeStatus(deps: {
  runtime: StudioRuntime;
}): RuntimeStatusApi {
  return createZustandRuntimeStatus(deps);
}

export function useRuntimeStatus(): RuntimeStatusSnapshot {
  const api = getFeature<RuntimeStatusApi>(RUNTIME_STATUS_KEY);
  return useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
}
