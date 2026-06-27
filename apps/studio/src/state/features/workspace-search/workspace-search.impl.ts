import type {
  WorkspaceSearch,
  WorkspaceSearchApi,
} from "./workspace-search.contract.js";

export interface WorkspaceSearchDeps {
  readonly port: WorkspaceSearch;
}

export function createWorkspaceSearchFeature(
  deps: WorkspaceSearchDeps,
): WorkspaceSearchApi {
  const { port } = deps;
  return {
    search: (workspacePath, query) => port.search(workspacePath, query),
  };
}
