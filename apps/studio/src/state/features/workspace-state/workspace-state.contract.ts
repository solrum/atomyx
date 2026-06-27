import type { WorkspaceState } from "../../../domain/features/workspace-state/index.js";

export type { WorkspaceState };

export interface WorkspaceStateSnapshot {
  readonly workspacePath: string | null;
  readonly state: WorkspaceState;
  readonly loaded: boolean;
}

export interface WorkspaceStateApi {
  getSnapshot(): WorkspaceStateSnapshot;
  subscribe(listener: () => void): () => void;
  hydrate(path: string): Promise<void>;
  recordRecentFile(path: string): void;
  flush(): Promise<void>;
  /**
   * Patch the in-memory `state` and flush to disk. Returns the
   * updated snapshot. No-op when nothing is loaded.
   */
  patch(next: WorkspaceState): void;
}
