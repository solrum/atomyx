export interface FileEntry {
  readonly path: string;
  readonly name: string;
  readonly type: "file" | "directory";
  readonly children?: readonly FileEntry[];
}

export interface FileTree {
  readonly rootPath: string;
  readonly entries: readonly FileEntry[];
}
