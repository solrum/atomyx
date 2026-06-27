import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  FileTree,
  WorkspaceApi,
  WorkspaceSnapshot,
} from "./workspace.contract.js";
import {
  createZustandWorkspace,
  type WorkspaceDeps,
} from "./workspace.zustand.js";

export type { FileTree, WorkspaceApi, WorkspaceSnapshot };

export const WORKSPACE_KEY = "workspace";

export function createWorkspace(deps: WorkspaceDeps): WorkspaceApi {
  return createZustandWorkspace(deps);
}

export function useWorkspace(): WorkspaceSnapshot &
  Pick<
    WorkspaceApi,
    | "pickAndOpen"
    | "openFolder"
    | "refresh"
    | "reloadTree"
    | "createScript"
    | "createFolder"
    | "renameScript"
    | "deleteScript"
  > {
  const api = getFeature<WorkspaceApi>(WORKSPACE_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    pickAndOpen: api.pickAndOpen,
    openFolder: api.openFolder,
    refresh: api.refresh,
    reloadTree: api.reloadTree,
    createScript: api.createScript,
    createFolder: api.createFolder,
    renameScript: api.renameScript,
    deleteScript: api.deleteScript,
  };
}
