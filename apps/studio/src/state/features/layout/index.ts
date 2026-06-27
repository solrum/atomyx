import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  LayoutApi,
  LayoutSnapshot,
  BottomPane,
  PaneSizes,
  ViewMode,
} from "./layout.contract.js";
import { DEFAULT_PANE_SIZES } from "./layout.contract.js";
import { createZustandLayout } from "./layout.zustand.js";

export type {
  LayoutApi,
  LayoutSnapshot,
  BottomPane,
  PaneSizes,
  ViewMode,
};
export { DEFAULT_PANE_SIZES };

export const LAYOUT_KEY = "layout";

export function createLayout(): LayoutApi {
  return createZustandLayout();
}

type LayoutActions = Omit<LayoutApi, "getSnapshot" | "subscribe">;

export function useLayout(): LayoutSnapshot & LayoutActions {
  const api = getFeature<LayoutApi>(LAYOUT_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    toggleFileTree: api.toggleFileTree,
    toggleRunDrawer: api.toggleRunDrawer,
    toggleRunDrawerCollapsed: api.toggleRunDrawerCollapsed,
    toggleInspector: api.toggleInspector,
    toggleStructure: api.toggleStructure,
    toggleProblems: api.toggleProblems,
    toggleTodos: api.toggleTodos,
    toggleTerminal: api.toggleTerminal,
    toggleHistory: api.toggleHistory,
    toggleLogs: api.toggleLogs,
    toggleScenario: api.toggleScenario,
    toggleZen: api.toggleZen,
    toggleSettingsView: api.toggleSettingsView,
    setSettingsView: api.setSettingsView,
    setFileTree: api.setFileTree,
    setRunDrawer: api.setRunDrawer,
    setRunDrawerCollapsed: api.setRunDrawerCollapsed,
    setInspector: api.setInspector,
    setProblems: api.setProblems,
    setBottomPane: api.setBottomPane,
    setPaneSize: api.setPaneSize,
    resetPaneSize: api.resetPaneSize,
    setViewMode: api.setViewMode,
  };
}
