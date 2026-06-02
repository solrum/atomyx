import type {
  WorkspaceSearchApi,
  WorkspaceSearchHit,
} from "./workspace-search.contract.js";
import {
  createWorkspaceSearchFeature,
  type WorkspaceSearchDeps,
} from "./workspace-search.impl.js";

export type { WorkspaceSearchApi, WorkspaceSearchHit };

export const WORKSPACE_SEARCH_KEY = "workspace-search";

export function createWorkspaceSearch(
  deps: WorkspaceSearchDeps,
): WorkspaceSearchApi {
  return createWorkspaceSearchFeature(deps);
}

