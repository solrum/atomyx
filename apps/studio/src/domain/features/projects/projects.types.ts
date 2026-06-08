/**
 * A workspace the user has opened before. Kept across restarts in
 * a global registry (not inside the workspace folder) so the
 * Welcome screen can list recent and pinned workspaces even when
 * the folder is not currently open.
 *
 * `id` is a stable hash of `path` — survives renames of the
 * display name, breaks if the folder moves (treated as a new
 * project, same as IntelliJ).
 */
export interface RecentProject {
  readonly id: string;
  readonly path: string;
  readonly displayName: string;
  readonly pinned: boolean;
  /** ms since epoch. Touched on every open; drives recency order. */
  readonly lastOpenedAt: number;
  /** ms since epoch. Set once at first registration; never updated. */
  readonly addedAt: number;
}

/**
 * Sort order the Welcome screen expects: pinned projects first
 * alphabetically, then unpinned by most-recent-open descending.
 * A pure function so the UI can trivially re-sort on any update.
 */
export function sortRecentProjects(
  projects: readonly RecentProject[],
): readonly RecentProject[] {
  return [...projects].sort((a, b) => {
    if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
    if (a.pinned && b.pinned) {
      return a.displayName.localeCompare(b.displayName);
    }
    return b.lastOpenedAt - a.lastOpenedAt;
  });
}
