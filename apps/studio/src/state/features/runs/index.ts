import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  LiveRun,
  RunEvent,
  RunMetadata,
  RunOpts,
  RunResult,
  RunsApi,
  RunsSnapshot,
} from "./runs.contract.js";
import { createZustandRuns, type RunsDeps } from "./runs.zustand.js";

export type {
  LiveRun,
  RunEvent,
  RunMetadata,
  RunOpts,
  RunResult,
  RunsApi,
  RunsSnapshot,
};

export const RUNS_KEY = "runs";

export function createRuns(deps: RunsDeps): RunsApi {
  return createZustandRuns(deps);
}

export function useRuns(): RunsSnapshot &
  Pick<RunsApi, "startRun" | "stopRun" | "loadHistory" | "deleteRun"> {
  const api = getFeature<RunsApi>(RUNS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    startRun: api.startRun,
    stopRun: api.stopRun,
    loadHistory: api.loadHistory,
    deleteRun: api.deleteRun,
  };
}
