import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  WorkspaceState,
  WorkspaceStateApi,
  WorkspaceStateSnapshot,
} from "./workspace-state.contract.js";
import {
  createZustandWorkspaceState,
  installWorkspaceStatePersistence as installPersistence,
  type WorkspaceStateDeps,
} from "./workspace-state.zustand.js";

export type { WorkspaceState, WorkspaceStateApi, WorkspaceStateSnapshot };

export const WORKSPACE_STATE_KEY = "workspace-state";

export function createWorkspaceState(
  deps: WorkspaceStateDeps,
): WorkspaceStateApi {
  return createZustandWorkspaceState(deps);
}

export function useWorkspaceState(): WorkspaceStateSnapshot &
  Pick<WorkspaceStateApi, "hydrate" | "recordRecentFile" | "flush" | "patch"> {
  const api = getFeature<WorkspaceStateApi>(WORKSPACE_STATE_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    hydrate: api.hydrate,
    recordRecentFile: api.recordRecentFile,
    flush: api.flush,
    patch: api.patch,
  };
}

export function installWorkspaceStatePersistence(): void {
  installPersistence(getFeature<WorkspaceStateApi>(WORKSPACE_STATE_KEY));
}
