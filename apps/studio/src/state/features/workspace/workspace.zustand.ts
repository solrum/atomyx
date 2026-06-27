import { createStore } from "zustand/vanilla";
import type { WorkspaceStore as WorkspacePort } from "../../../domain/features/workspace/index.js";
import type { WorkspaceApi, WorkspaceSnapshot } from "./workspace.contract.js";

export interface WorkspaceDeps {
  readonly store: WorkspacePort;
}

export function createZustandWorkspace(deps: WorkspaceDeps): WorkspaceApi {
  const { store: port } = deps;
  const store = createStore<WorkspaceSnapshot>(() => ({
    currentPath: null,
    tree: null,
    loading: false,
    error: null,
  }));

  const api: WorkspaceApi = {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    async pickAndOpen() {
      const path = await port.pickFolder();
      if (!path) return;
      await api.openFolder(path);
    },

    async openFolder(path) {
      store.setState({ loading: true, error: null });
      try {
        const tree = await port.openFolder(path);
        store.setState({ currentPath: path, tree, loading: false });
        // Bubble the project to the top of the recent list, refresh
        // workspace-local themes, and restore per-workspace UI
        // state (tabs, tool-window visibility, recent files).
        const [
          themesMod,
          projectsMod,
          workspaceStateMod,
          runConfigsMod,
        ] = await Promise.all([
          import("../theme/index.js"),
          import("../projects/index.js"),
          import("../workspace-state/index.js"),
          import("../run-configs/index.js"),
        ]);
        const { getFeature: gf } = await import("../../core/registry.js");
        await Promise.all([
          gf<import("../theme/index.js").ThemeApi>(themesMod.THEME_KEY).reload(path),
          gf<import("../projects/index.js").ProjectsApi>(projectsMod.PROJECTS_KEY).touch(path),
          gf<import("../workspace-state/index.js").WorkspaceStateApi>(workspaceStateMod.WORKSPACE_STATE_KEY).hydrate(path),
        ]);
        // Run configs need workspace-state to land first so its
        // `lastActiveRunConfig` is read correctly.
        await gf<import("../run-configs/index.js").RunConfigsApi>(runConfigsMod.RUN_CONFIGS_KEY).hydrate(path);
      } catch (err) {
        store.setState({
          loading: false,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    },

    async refresh() {
      const { currentPath } = store.getState();
      if (!currentPath) return;
      await api.openFolder(currentPath);
    },

    async reloadTree() {
      const { currentPath } = store.getState();
      if (!currentPath) return;
      const tree = await port.openFolder(currentPath);
      store.setState({ tree });
    },

    createScript(parentPath, fileName, content) {
      return port.createScript(parentPath, fileName, content);
    },

    createFolder(parentPath, folderName) {
      return port.createFolder(parentPath, folderName);
    },

    renameScript(path, newName) {
      return port.renameScript(path, newName);
    },

    deleteScript(path) {
      return port.deleteScript(path);
    },
  };

  return api;
}
