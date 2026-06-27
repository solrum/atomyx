export type FsChangeKind = "created" | "modified" | "removed";

export interface FsChangeEvent {
  readonly kind: FsChangeKind;
  readonly paths: readonly string[];
}

export type FsChangeListener = (event: FsChangeEvent) => void;

export interface WorkspaceWatcher {
  start(workspacePath: string, listener: FsChangeListener): Promise<void>;
  stop(): Promise<void>;
}
