import type {
  WorkspaceSearch,
  WorkspaceSearchHit,
} from "../../../domain/features/workspace-search/index.js";

export type { WorkspaceSearch, WorkspaceSearchHit };

export interface WorkspaceSearchApi {
  search(
    workspacePath: string,
    query: string,
  ): Promise<readonly WorkspaceSearchHit[]>;
}
