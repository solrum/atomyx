import { createStore } from "zustand/vanilla";
import { getFeature } from "../../core/registry.js";
import type { EditorApi } from "../editor/index.js";
import { EDITOR_KEY } from "../editor/index.js";
import type { WorkspaceApi } from "../workspace/index.js";
import { WORKSPACE_KEY } from "../workspace/index.js";
import type { RunsApi } from "../runs/index.js";
import { RUNS_KEY } from "../runs/index.js";
import type { ThemeApi } from "../theme/index.js";
import { THEME_KEY } from "../theme/index.js";
import type { LayoutApi } from "../layout/index.js";
import { LAYOUT_KEY } from "../layout/index.js";
import type { NotificationsApi } from "../notifications/index.js";
import { NOTIFICATIONS_KEY } from "../notifications/index.js";
import { getServices } from "../../core/services.js";
import type {
  ActionHandler,
  ActionsApi,
  ActionsSnapshot,
} from "./actions.contract.js";
import { ACTION_DEFINITIONS } from "./actions.definitions.js";

export function createZustandActions(): ActionsApi {
  const store = createStore<ActionsSnapshot>(() => ({
    definitions: ACTION_DEFINITIONS,
    paletteOpen: false,
    paletteQuery: "",
  }));

  const uiHandlers = new Map<string, ActionHandler>();

  const api: ActionsApi = {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    registerHandler(id, handler) {
      uiHandlers.set(id, handler);
      return () => {
        if (uiHandlers.get(id) === handler) uiHandlers.delete(id);
      };
    },

    openPalette() {
      store.setState({ paletteOpen: true, paletteQuery: "" });
    },

    closePalette() {
      store.setState({ paletteOpen: false, paletteQuery: "" });
    },

    setQuery(query) {
      store.setState({ paletteQuery: query });
    },

    async execute(id) {
      try {
        await executeAction(id, uiHandlers, api);
      } finally {
        store.setState({ paletteOpen: false, paletteQuery: "" });
      }
    },
  };

  return api;
}

async function executeAction(
  id: string,
  uiHandlers: Map<string, ActionHandler>,
  api: ActionsApi,
): Promise<void> {
  const uiHandler = uiHandlers.get(id);
  if (uiHandler) {
    await uiHandler();
    return;
  }
  switch (id) {
    case "workbench.action.showPalette": {
      api.openPalette();
      return;
    }
    case "workbench.action.toggleFileTree": {
      getFeature<LayoutApi>(LAYOUT_KEY).toggleFileTree();
      return;
    }
    case "workbench.action.toggleRunPanel": {
      getFeature<LayoutApi>(LAYOUT_KEY).toggleRunDrawer();
      return;
    }
    case "workbench.action.toggleStructure": {
      getFeature<LayoutApi>(LAYOUT_KEY).toggleStructure();
      return;
    }
    case "workbench.action.toggleProblems": {
      getFeature<LayoutApi>(LAYOUT_KEY).toggleProblems();
      return;
    }
    case "workbench.action.toggleTodos": {
      getFeature<LayoutApi>(LAYOUT_KEY).toggleTodos();
      const todosModule = await import("../todos/index.js");
      void getFeature<import("../todos/index.js").TodosApi>(todosModule.TODOS_KEY).refresh();
      return;
    }
    case "workbench.action.toggleTerminal": {
      getFeature<LayoutApi>(LAYOUT_KEY).toggleTerminal();
      return;
    }
    case "workbench.action.toggleZen": {
      getFeature<LayoutApi>(LAYOUT_KEY).toggleZen();
      return;
    }
    case "file.save": {
      await getFeature<EditorApi>(EDITOR_KEY).saveActive();
      return;
    }
    case "file.saveAll": {
      await getFeature<EditorApi>(EDITOR_KEY).saveAll();
      return;
    }
    case "file.close": {
      const editorApi = getFeature<EditorApi>(EDITOR_KEY);
      const active = editorApi.getSnapshot().activePath;
      if (active) editorApi.closeFile(active);
      return;
    }
    case "file.reopenClosed": {
      await getFeature<EditorApi>(EDITOR_KEY).reopenLastClosed();
      return;
    }
    case "file.closeOthers": {
      const editorApi = getFeature<EditorApi>(EDITOR_KEY);
      const active = editorApi.getSnapshot().activePath;
      if (active) editorApi.closeOthers(active);
      return;
    }
    case "file.closeToRight": {
      const editorApi = getFeature<EditorApi>(EDITOR_KEY);
      const active = editorApi.getSnapshot().activePath;
      if (active) editorApi.closeToRight(active);
      return;
    }
    case "file.closeAll": {
      getFeature<EditorApi>(EDITOR_KEY).closeAll();
      return;
    }
    case "editor.nextTab": {
      getFeature<EditorApi>(EDITOR_KEY).nextTab();
      return;
    }
    case "editor.previousTab": {
      getFeature<EditorApi>(EDITOR_KEY).previousTab();
      return;
    }
    case "editor.splitRight": {
      getFeature<EditorApi>(EDITOR_KEY).splitRight();
      return;
    }
    case "editor.closeSplit": {
      const editorApi = getFeature<EditorApi>(EDITOR_KEY);
      const gid = editorApi.getSnapshot().activeGroupId;
      editorApi.closeGroup(gid);
      return;
    }
    case "workspace.openFolder": {
      await getFeature<WorkspaceApi>(WORKSPACE_KEY).pickAndOpen();
      return;
    }
    case "workspace.reloadThemes": {
      await getFeature<ThemeApi>(THEME_KEY).reload();
      return;
    }
    case "workspace.openThemesDir": {
      await getServices().themes.openThemesDir();
      return;
    }
    case "theme.clearOverrides": {
      await getFeature<ThemeApi>(THEME_KEY).clearOverrides();
      return;
    }
    case "run.start": {
      const devicesModule = await import("../devices/index.js");
      const runConfigsModule = await import("../run-configs/index.js");
      const editorApi = getFeature<EditorApi>(EDITOR_KEY);
      const editorSnap = editorApi.getSnapshot();
      const activeTab = editorSnap.tabs.find(
        (t) => t.path === editorSnap.activePath,
      );
      const notifApi = getFeature<NotificationsApi>(NOTIFICATIONS_KEY);
      if (!activeTab) {
        notifApi.show({
          kind: "warn",
          title: "No script to run",
          detail: "Open a YAML tab first, then press Run.",
        });
        return;
      }
      const deviceId = getFeature<import("../devices/index.js").DevicesApi>(
        devicesModule.DEVICES_KEY,
      ).getSnapshot().selectedId;
      if (!deviceId) {
        notifApi.show({
          kind: "warn",
          title: "No device selected",
          detail: "Pick a device in the toolbar dropdown first.",
        });
        return;
      }
      const rcSnap = getFeature<import("../run-configs/index.js").RunConfigsApi>(
        runConfigsModule.RUN_CONFIGS_KEY,
      ).getSnapshot();
      const activeConfig = rcSnap.configs.find((c) => c.id === rcSnap.activeId);
      await getFeature<RunsApi>(RUNS_KEY).startRun(activeTab.path, activeTab.content, {
        deviceId,
        env: activeConfig?.env,
      });
      return;
    }
    case "run.stop": {
      getFeature<RunsApi>(RUNS_KEY).stopRun();
      return;
    }
    default:
      throw new Error(`Unknown action id: ${id}`);
  }
}
