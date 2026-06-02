import { invoke } from "@tauri-apps/api/core";
import { open } from "@tauri-apps/plugin-dialog";
import type { FileTree } from "../../../domain/features/workspace/index.js";
import type { WorkspaceStore } from "../../../domain/features/workspace/index.js";

/**
 * Filesystem-backed workspace store. Reads / writes script files
 * through the Rust backend; folder-picker dialog via the Tauri
 * dialog plugin directly from the renderer (it returns a string,
 * no binary data, so delegating to Rust adds no benefit).
 *
 * Path handling is the backend's job — this adapter passes strings
 * through and trusts the backend to reject escapes out of the
 * opened workspace root.
 */
export class TauriWorkspaceStore implements WorkspaceStore {
  async openFolder(path: string): Promise<FileTree> {
    return invoke<FileTree>("workspace_open_folder", { path });
  }

  async readScript(path: string): Promise<string> {
    return invoke<string>("workspace_read_script", { path });
  }

  async writeScript(path: string, content: string): Promise<void> {
    await invoke("workspace_write_script", { path, content });
  }

  async createScript(
    parentPath: string,
    fileName: string,
    content: string,
  ): Promise<string> {
    return invoke<string>("workspace_create_script", {
      parentPath,
      fileName,
      content,
    });
  }

  async createFolder(parentPath: string, folderName: string): Promise<string> {
    return invoke<string>("workspace_create_directory", {
      parentPath,
      folderName,
    });
  }

  async deleteScript(path: string): Promise<void> {
    await invoke("workspace_delete_script", { path });
  }

  async renameScript(path: string, newName: string): Promise<string> {
    return invoke<string>("workspace_rename_script", { path, newName });
  }

  async pickFolder(): Promise<string | null> {
    const result = await open({
      directory: true,
      multiple: false,
      title: "Open Atomyx workspace",
    });
    if (typeof result === "string") return result;
    return null;
  }
}
