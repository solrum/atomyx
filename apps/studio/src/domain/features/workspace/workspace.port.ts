import type { FileTree } from "./types.js";

/**
 * Filesystem operations the editor and file tree need. Keeps the
 * UI layer ignorant of the underlying plugin — a remote-workspace
 * adapter (SSH, cloud) can later implement the same contract
 * without touching the editor.
 */
export interface WorkspaceStore {
  openFolder(path: string): Promise<FileTree>;
  readScript(path: string): Promise<string>;
  writeScript(path: string, content: string): Promise<void>;
  createScript(
    parentPath: string,
    fileName: string,
    content: string,
  ): Promise<string>;
  createFolder(parentPath: string, folderName: string): Promise<string>;
  deleteScript(path: string): Promise<void>;
  renameScript(path: string, newName: string): Promise<string>;
  /**
   * Open a native directory-picker dialog. Resolved with the
   * chosen path, or `null` if the user cancelled.
   */
  pickFolder(): Promise<string | null>;
}
