import { invoke } from "@tauri-apps/api/core";
import type { ProjectConfigStore } from "../../../domain/features/project-config/index.js";

/**
 * Tauri-backed ProjectConfigStore. Every call routes through a
 * Rust command that validates `relPath` (no absolute paths, no
 * `..`) before touching disk — path-escape rejection lives on the
 * backend so future renderers can't bypass it.
 */
export class TauriProjectConfigStore implements ProjectConfigStore {
  async readJson<T>(
    workspacePath: string,
    relPath: string,
  ): Promise<T | null> {
    return invoke<T | null>("project_config_read_json", {
      workspacePath,
      relPath,
    });
  }

  async writeJson<T>(
    workspacePath: string,
    relPath: string,
    value: T,
  ): Promise<void> {
    await invoke("project_config_write_json", {
      workspacePath,
      relPath,
      value,
    });
  }

  async readText(
    workspacePath: string,
    relPath: string,
  ): Promise<string | null> {
    return invoke<string | null>("project_config_read_text", {
      workspacePath,
      relPath,
    });
  }

  async writeText(
    workspacePath: string,
    relPath: string,
    content: string,
  ): Promise<void> {
    await invoke("project_config_write_text", {
      workspacePath,
      relPath,
      content,
    });
  }

  async listJsonDirectory(
    workspacePath: string,
    relPath: string,
  ): Promise<readonly unknown[]> {
    return invoke<readonly unknown[]>(
      "project_config_list_json_directory",
      { workspacePath, relPath },
    );
  }
}
