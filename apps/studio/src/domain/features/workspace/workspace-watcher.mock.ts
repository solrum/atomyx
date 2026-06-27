import type {
  FsChangeListener,
  WorkspaceWatcher,
} from "./workspace-watcher.port.js";

export class MockWorkspaceWatcher implements WorkspaceWatcher {
  private listener: FsChangeListener | null = null;
  private watchedPath: string | null = null;

  async start(workspacePath: string, listener: FsChangeListener): Promise<void> {
    this.watchedPath = workspacePath;
    this.listener = listener;
  }

  async stop(): Promise<void> {
    this.listener = null;
    this.watchedPath = null;
  }

  emit(event: Parameters<FsChangeListener>[0]): void {
    this.listener?.(event);
  }

  get currentPath(): string | null {
    return this.watchedPath;
  }
}
