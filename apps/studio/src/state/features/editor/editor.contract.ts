export interface EditorTab {
  readonly path: string;
  readonly content: string;
  readonly savedContent: string;
  readonly dirty: boolean;
  readonly pinned: boolean;
}

export interface EditorGroup {
  readonly id: string;
  readonly tabs: readonly EditorTab[];
  readonly activePath: string | null;
}

export interface EditorSnapshot {
  readonly groups: readonly EditorGroup[];
  readonly activeGroupId: string;
  /** Flat view of the active group's tabs for consumers that do not manage groups themselves. */
  readonly tabs: readonly EditorTab[];
  /** Flat view of the active group's activePath. */
  readonly activePath: string | null;
  readonly closedStack: readonly string[];
}

export interface EditorApi {
  getSnapshot(): EditorSnapshot;
  subscribe(listener: () => void): () => void;
  openFile(path: string): Promise<void>;
  closeFile(path: string): void;
  closeOthers(keepPath: string): void;
  closeToRight(keepPath: string): void;
  closeAll(): void;
  activate(path: string): void;
  updateContent(path: string, content: string): void;
  saveActive(): Promise<void>;
  saveAll(): Promise<void>;
  reopenLastClosed(): Promise<void>;
  renameTab(oldPath: string, newPath: string): void;
  nextTab(): void;
  previousTab(): void;
  togglePinned(path: string): void;
  reloadFromDisk(path: string, contents: string): void;
  splitRight(): void;
  closeGroup(groupId: string): void;
  focusGroup(groupId: string): void;
}
