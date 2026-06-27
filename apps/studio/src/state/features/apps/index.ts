import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type { StudioRuntime } from "../../../domain/features/runtime/index.js";
import type { AppsApi, AppsSnapshot, App } from "./apps.contract.js";
import { createZustandApps } from "./apps.zustand.js";

export type { AppsApi, AppsSnapshot, App };

export const APPS_KEY = "apps";

export function createApps(deps: { runtime: StudioRuntime }): AppsApi {
  return createZustandApps(deps);
}

export function useApps(): AppsSnapshot & { readonly refresh: AppsApi["refresh"] } {
  const api = getFeature<AppsApi>(APPS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return { ...snap, refresh: api.refresh };
}
