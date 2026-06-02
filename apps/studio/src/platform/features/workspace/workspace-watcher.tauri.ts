import { invoke, Channel } from "@tauri-apps/api/core";
import type {
  FsChangeEvent,
  FsChangeListener,
  WorkspaceWatcher,
} from "../../../domain/features/workspace/index.js";

export class TauriWorkspaceWatcher implements WorkspaceWatcher {
  async start(
    workspacePath: string,
    listener: FsChangeListener,
  ): Promise<void> {
    const channel = new Channel<FsChangeEvent>();
    channel.onmessage = (evt) => listener(evt);
    await invoke("workspace_watch", {
      workspacePath,
      onEvent: channel,
    });
  }

  async stop(): Promise<void> {
    await invoke("workspace_unwatch");
  }
}
