import type { RecentProject } from "./projects.types.js";

/**
 * Persistence boundary for the recent-projects list. The Welcome
 * screen and the workspace-open flow are the only two callers:
 * Welcome lists + mutates, workspace-open calls `touch` to bubble
 * the project to the top.
 */
export interface ProjectRegistry {
  list(): Promise<readonly RecentProject[]>;
  /**
   * Register a project (if new) or bump its `lastOpenedAt` (if
   * known). Returns the resulting record. Display name is derived
   * from the path's basename on first registration; subsequent
   * touches do not rename.
   */
  touch(path: string): Promise<RecentProject>;
  setPinned(id: string, pinned: boolean): Promise<void>;
  remove(id: string): Promise<void>;
}
