import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  RunConfig,
  RunConfigsApi,
  RunConfigsSnapshot,
} from "./run-configs.contract.js";
import {
  createZustandRunConfigs,
  type RunConfigsDeps,
} from "./run-configs.zustand.js";

export type { RunConfig, RunConfigsApi, RunConfigsSnapshot };

export const RUN_CONFIGS_KEY = "run-configs";

export function createRunConfigs(deps: RunConfigsDeps): RunConfigsApi {
  return createZustandRunConfigs(deps);
}

export function useRunConfigs(): RunConfigsSnapshot &
  Pick<
    RunConfigsApi,
    "hydrate" | "setActive" | "save" | "remove" | "duplicate"
  > {
  const api = getFeature<RunConfigsApi>(RUN_CONFIGS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    hydrate: api.hydrate,
    setActive: api.setActive,
    save: api.save,
    remove: api.remove,
    duplicate: api.duplicate,
  };
}
