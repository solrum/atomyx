import type { FileTree } from "../../../domain/features/workspace/index.js";

export type { FileTree };

export interface WorkspaceSnapshot {
  readonly currentPath: string | null;
  readonly tree: FileTree | null;
  readonly loading: boolean;
  readonly error: string | null;
}

export interface WorkspaceApi {
  getSnapshot(): WorkspaceSnapshot;
  subscribe(listener: () => void): () => void;
  pickAndOpen(): Promise<void>;
  openFolder(path: string): Promise<void>;
  refresh(): Promise<void>;
  reloadTree(): Promise<void>;
  createScript(
    parentPath: string,
    fileName: string,
    content: string,
  ): Promise<string>;
  createFolder(parentPath: string, folderName: string): Promise<string>;
  renameScript(path: string, newName: string): Promise<string>;
  deleteScript(path: string): Promise<void>;
}
