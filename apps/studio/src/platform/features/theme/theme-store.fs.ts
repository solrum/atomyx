import { Channel, invoke } from "@tauri-apps/api/core";
import type {
  RawTheme,
  ThemeStore,
  ThemeWatchCallback,
} from "../../../domain/features/theme/index.js";
import type { Theme } from "../../../domain/features/theme/index.js";
import type { ProjectConfigStore } from "../../../domain/features/project-config/index.js";

export interface FsThemeStoreDeps {
  readonly projectConfig: ProjectConfigStore;
}

/**
 * Filesystem-backed theme store. Talks to the Tauri Rust backend
 * for both the bundled built-in JSONs (compiled into the binary
 * via `include_str!`) and the user-editable folder under
 * `~/Library/Application Support/dev.atomyx.studio/themes/`.
 *
 * Workspace-scoped themes live at `<workspace>/.atomyx/themes/`
 * and route through the project-config port — the single
 * `.atomyx/` access point for every workspace config file — so
 * the theme store doesn't carry its own copy of path-escape
 * validation or `.atomyx/` directory handling.
 *
 * `watch` bridges a Rust `notify` watcher back to the caller
 * through a Tauri Channel: any add / modify / delete under the
 * user themes folder fires the callback, which the theme store
 * uses to re-run discovery without a manual reload.
 */
export class FsThemeStore implements ThemeStore {
  constructor(private readonly deps: FsThemeStoreDeps) {}

  async listBuiltIns(): Promise<readonly RawTheme[]> {
    const jsons = await invoke<readonly unknown[]>("themes_list_builtin");
    return jsons.map((json) => ({
      source: "built-in" as const,
      path: null,
      json,
    }));
  }

  async listUser(): Promise<readonly RawTheme[]> {
    const jsons = await invoke<readonly unknown[]>("themes_list_user");
    return jsons.map((json) => ({
      source: "user" as const,
      path: null,
      json,
    }));
  }

  async listWorkspace(workspacePath: string): Promise<readonly RawTheme[]> {
    const jsons = await this.deps.projectConfig.listJsonDirectory(
      workspacePath,
      "themes",
    );
    return jsons.map((json) => ({
      source: "workspace" as const,
      path: null,
      json,
    }));
  }

  async loadById(id: string): Promise<RawTheme | null> {
    const json = await invoke<unknown | null>("themes_read", { id });
    if (!json) return null;
    return { source: "user", path: null, json };
  }

  async saveUser(theme: Theme): Promise<void> {
    await invoke("themes_write", { theme });
  }

  async deleteUser(id: string): Promise<void> {
    await invoke("themes_delete", { id });
  }

  async openThemesDir(): Promise<void> {
    await invoke("themes_open_dir");
  }

  watch(callback: ThemeWatchCallback): () => void {
    const channel = new Channel<unknown>();
    let disposed = false;

    channel.onmessage = async () => {
      if (disposed) return;
      // We don't know exactly what changed — the theme store will
      // re-discover and diff. Fire a synthetic event; the caller
      // is expected to reload.
      try {
        const users = await this.listUser();
        for (const raw of users) {
          const id = (raw.json as { id?: string }).id;
          const label = (raw.json as { label?: string }).label ?? id ?? "theme";
          if (typeof id !== "string") continue;
          callback({
            type: "updated",
            theme: {
              schemaVersion: 1,
              id,
              label,
              monacoBase: "vs-dark",
              attributes: {},
            },
          });
        }
      } catch {
        /* swallow — watcher best-effort */
      }
    };

    void invoke("themes_watch", { onEvent: channel }).catch(() => {
      // Watcher install may fail on exotic filesystems; the
      // "Reload" action remains as a manual fallback.
    });

    return () => {
      disposed = true;
    };
  }
}
