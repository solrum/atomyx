import type { RecentProject } from "../../../domain/features/projects/index.js";

export type { RecentProject };

export interface ProjectsSnapshot {
  readonly items: readonly RecentProject[];
}

export interface ProjectsApi {
  getSnapshot(): ProjectsSnapshot;
  subscribe(listener: () => void): () => void;
  reload(): Promise<void>;
  touch(path: string): Promise<RecentProject>;
  setPinned(id: string, pinned: boolean): Promise<void>;
  remove(id: string): Promise<void>;
}
