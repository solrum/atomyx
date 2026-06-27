import { invoke } from "@tauri-apps/api/core";
import type {
  WorkspaceSearch,
  WorkspaceSearchHit,
} from "../../../domain/features/workspace-search/index.js";

/**
 * Tauri-backed grep. Delegates to the Rust `workspace_search`
 * command which walks the opened workspace root and returns
 * line-level matches. Caps + exclusions live in the backend so
 * the renderer stays free of filesystem knowledge.
 */
export class TauriWorkspaceSearch implements WorkspaceSearch {
  async search(
    workspacePath: string,
    query: string,
  ): Promise<readonly WorkspaceSearchHit[]> {
    return invoke<readonly WorkspaceSearchHit[]>("workspace_search", {
      workspacePath,
      query,
    });
  }
}
