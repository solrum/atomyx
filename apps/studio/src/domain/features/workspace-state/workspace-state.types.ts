/**
 * Per-workspace UI state persisted under
 * `<workspace>/.atomyx/workspace.json`. Reopening the workspace
 * later restores tabs, active file, tool-window visibility, and
 * recent-files list — the user never has to rebuild context.
 *
 * Versioned so future shape changes can migrate gracefully.
 */
export const WORKSPACE_STATE_SCHEMA_VERSION = 1;

export interface Bookmark {
  readonly path: string;
  readonly line: number;
  readonly note?: string;
  readonly createdAt: number;
}

export interface WorkspaceState {
  readonly schemaVersion: number;
  readonly openTabs: readonly string[];
  readonly pinnedTabs?: readonly string[];
  readonly activePath: string | null;
  readonly layout: {
    readonly fileTreeVisible: boolean;
    readonly runPanelVisible: boolean;
  };
  /** Most-recent-first list of file paths the user opened. Capped. */
  readonly recentFiles: readonly string[];
  readonly lastActiveRunConfig?: string | null;
  readonly bookmarks?: readonly Bookmark[];
}

export const EMPTY_WORKSPACE_STATE: WorkspaceState = {
  schemaVersion: WORKSPACE_STATE_SCHEMA_VERSION,
  openTabs: [],
  activePath: null,
  layout: {
    fileTreeVisible: true,
    runPanelVisible: true,
  },
  recentFiles: [],
  lastActiveRunConfig: null,
  bookmarks: [],
};

export const RECENT_FILES_CAP = 20;
export const BOOKMARKS_CAP = 200;
