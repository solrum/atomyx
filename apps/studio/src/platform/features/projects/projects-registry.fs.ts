import { invoke } from "@tauri-apps/api/core";
import type { ProjectRegistry } from "../../../domain/features/projects/index.js";
import type { RecentProject } from "../../../domain/features/projects/index.js";

/**
 * Filesystem-backed recent-projects registry. Delegates to Rust,
 * which owns the JSON file at
 * `~/Library/Application Support/dev.atomyx.studio/recent-projects.json`.
 * Stays outside any workspace folder so it survives across
 * projects and is never committed to a user's git history.
 */
export class FsProjectRegistry implements ProjectRegistry {
  async list(): Promise<readonly RecentProject[]> {
    return invoke<readonly RecentProject[]>("projects_list");
  }

  async touch(path: string): Promise<RecentProject> {
    return invoke<RecentProject>("projects_touch", { path });
  }

  async setPinned(id: string, pinned: boolean): Promise<void> {
    await invoke("projects_set_pinned", { id, pinned });
  }

  async remove(id: string): Promise<void> {
    await invoke("projects_remove", { id });
  }
}
