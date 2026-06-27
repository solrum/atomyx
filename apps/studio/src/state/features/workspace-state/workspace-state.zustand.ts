import { createStore } from "zustand/vanilla";
import {
  EMPTY_WORKSPACE_STATE,
  RECENT_FILES_CAP,
  type WorkspaceState,
} from "../../../domain/features/workspace-state/index.js";
import { getFeature } from "../../core/registry.js";
import type { EditorApi } from "../editor/index.js";
import { EDITOR_KEY } from "../editor/index.js";
import type { LayoutApi } from "../layout/index.js";
import { LAYOUT_KEY } from "../layout/index.js";
import type { ProjectConfigApi } from "../project-config/index.js";
import type {
  WorkspaceStateApi,
  WorkspaceStateSnapshot,
} from "./workspace-state.contract.js";

const WORKSPACE_STATE_FILE = "workspace.json";

export interface WorkspaceStateDeps {
  readonly projectConfig: ProjectConfigApi;
}

export function createZustandWorkspaceState(
  deps: WorkspaceStateDeps,
): WorkspaceStateApi {
  const { projectConfig } = deps;
  const store = createStore<WorkspaceStateSnapshot>(() => ({
    workspacePath: null,
    state: EMPTY_WORKSPACE_STATE,
    loaded: false,
  }));

  const api: WorkspaceStateApi = {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    async hydrate(path) {
      const loaded = await projectConfig.readJson<WorkspaceState>(
        WORKSPACE_STATE_FILE,
      );
      const effective = loaded ?? EMPTY_WORKSPACE_STATE;
      store.setState({ workspacePath: path, state: effective, loaded: true });

      const layoutApi = getFeature<LayoutApi>(LAYOUT_KEY);
      const editorApi = getFeature<EditorApi>(EDITOR_KEY);

      // Apply tool-window visibility.
      layoutApi.setFileTree(effective.layout.fileTreeVisible);
      layoutApi.setRunDrawer(effective.layout.runPanelVisible);

      // Restore tabs in order. Each openFile is async → await sequentially
      // so activePath wins at the end.
      for (const tabPath of effective.openTabs) {
        try {
          await editorApi.openFile(tabPath);
        } catch {
          /* file deleted since last session — skip */
        }
      }
      const pinned = new Set(effective.pinnedTabs ?? []);
      if (pinned.size > 0) {
        for (const tab of editorApi.getSnapshot().tabs) {
          if (pinned.has(tab.path)) {
            editorApi.togglePinned(tab.path);
          }
        }
      }
      if (
        effective.activePath &&
        editorApi.getSnapshot().tabs.some((t) => t.path === effective.activePath)
      ) {
        editorApi.activate(effective.activePath);
      }
    },

    recordRecentFile(path) {
      const snap = store.getState();
      const filtered = snap.state.recentFiles.filter((p) => p !== path);
      const next: WorkspaceState = {
        ...snap.state,
        recentFiles: [path, ...filtered].slice(0, RECENT_FILES_CAP),
      };
      store.setState({ state: next });
      void api.flush();
    },

    async flush() {
      const { workspacePath, state } = store.getState();
      if (!workspacePath) return;
      await projectConfig.writeJson(WORKSPACE_STATE_FILE, state);
    },

    patch(next) {
      const snap = store.getState();
      if (!snap.workspacePath || !snap.loaded) return;
      store.setState({ state: next });
      void api.flush();
    },
  };

  return api;
}

export function installWorkspaceStatePersistence(
  api: WorkspaceStateApi,
): void {
  const editorApi = getFeature<EditorApi>(EDITOR_KEY);
  const layoutApi = getFeature<LayoutApi>(LAYOUT_KEY);

  editorApi.subscribe(() => {
    const snap = api.getSnapshot();
    if (!snap.workspacePath || !snap.loaded) return;
    const editorSnap = editorApi.getSnapshot();
    const openTabs = editorSnap.tabs.map((t) => t.path);
    const pinnedTabs = editorSnap.tabs
      .filter((t) => t.pinned)
      .map((t) => t.path);
    const next: WorkspaceState = {
      ...snap.state,
      openTabs,
      pinnedTabs,
      activePath: editorSnap.activePath,
    };
    const pinnedSame =
      (next.pinnedTabs?.join("|") ?? "") ===
      (snap.state.pinnedTabs?.join("|") ?? "");
    if (
      next.openTabs.join("|") !== snap.state.openTabs.join("|") ||
      next.activePath !== snap.state.activePath ||
      !pinnedSame
    ) {
      api.patch(next);
    }
  });

  layoutApi.subscribe(() => {
    const snap = api.getSnapshot();
    if (!snap.workspacePath || !snap.loaded) return;
    const l = layoutApi.getSnapshot();
    const next: WorkspaceState = {
      ...snap.state,
      layout: {
        fileTreeVisible: l.fileTreeVisible,
        runPanelVisible: l.runDrawerVisible,
      },
    };
    if (
      next.layout.fileTreeVisible !== snap.state.layout.fileTreeVisible ||
      next.layout.runPanelVisible !== snap.state.layout.runPanelVisible
    ) {
      api.patch(next);
    }
  });
}
