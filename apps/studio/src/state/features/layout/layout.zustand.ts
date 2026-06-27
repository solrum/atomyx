import { createStore } from "zustand/vanilla";
import type {
  BottomPane,
  LayoutApi,
  LayoutSnapshot,
  PaneSizes,
  ViewMode,
} from "./layout.contract.js";
import { DEFAULT_PANE_SIZES } from "./layout.contract.js";

const MIN_PANE: Record<keyof PaneSizes, number> = {
  fileTreeWidth: 160,
  mirrorWidth: 240,
  runDrawerHeight: 140,
  inspectorWidth: 220,
  bottomPaneHeight: 100,
};

export function createZustandLayout(): LayoutApi {
  const store = createStore<LayoutSnapshot>(() => ({
    fileTreeVisible: true,
    runDrawerVisible: true,
    runDrawerCollapsed: false,
    inspectorVisible: false,
    structureVisible: false,
    problemsVisible: false,
    bottomPane: "problems",
    zenMode: false,
    settingsViewVisible: false,
    viewMode: "author",
    paneSizes: DEFAULT_PANE_SIZES,
  }));

  const toggleBottom = (target: BottomPane) => {
    const { problemsVisible, bottomPane } = store.getState();
    if (problemsVisible && bottomPane === target) {
      store.setState({ problemsVisible: false });
    } else {
      store.setState({ problemsVisible: true, bottomPane: target });
    }
  };

  return {
    getSnapshot: () => store.getState(),
    subscribe: (l) => store.subscribe(l),

    toggleFileTree: () =>
      store.setState({ fileTreeVisible: !store.getState().fileTreeVisible }),
    toggleRunDrawer: () =>
      store.setState({ runDrawerVisible: !store.getState().runDrawerVisible }),
    toggleRunDrawerCollapsed: () =>
      store.setState({
        runDrawerCollapsed: !store.getState().runDrawerCollapsed,
      }),
    toggleInspector: () =>
      store.setState({
        inspectorVisible: !store.getState().inspectorVisible,
      }),
    toggleStructure: () =>
      store.setState({ structureVisible: !store.getState().structureVisible }),
    toggleProblems: () => toggleBottom("problems"),
    toggleTodos: () => toggleBottom("todos"),
    toggleTerminal: () => toggleBottom("terminal"),
    toggleHistory: () => toggleBottom("history"),
    toggleLogs: () => toggleBottom("logs"),
    toggleScenario: () => toggleBottom("scenario"),
    toggleZen: () => store.setState({ zenMode: !store.getState().zenMode }),
    toggleSettingsView: () =>
      store.setState({
        settingsViewVisible: !store.getState().settingsViewVisible,
      }),
    setSettingsView: (visible) =>
      store.setState({ settingsViewVisible: visible }),

    setFileTree: (visible) => store.setState({ fileTreeVisible: visible }),
    setRunDrawer: (visible) => store.setState({ runDrawerVisible: visible }),
    setRunDrawerCollapsed: (collapsed) =>
      store.setState({ runDrawerCollapsed: collapsed }),
    setInspector: (visible) =>
      store.setState({ inspectorVisible: visible }),
    setProblems: (visible) => store.setState({ problemsVisible: visible }),
    setBottomPane: (pane) => store.setState({ bottomPane: pane }),

    setPaneSize: (key: keyof PaneSizes, value: number) => {
      const clamped = Math.max(MIN_PANE[key], Math.round(value));
      const current = store.getState().paneSizes;
      if (current[key] === clamped) return;
      store.setState({ paneSizes: { ...current, [key]: clamped } });
    },
    resetPaneSize: (key: keyof PaneSizes) => {
      const current = store.getState().paneSizes;
      store.setState({ paneSizes: { ...current, [key]: DEFAULT_PANE_SIZES[key] } });
    },

    setViewMode: (mode: ViewMode) => {
      if (store.getState().viewMode === mode) return;
      store.setState({ viewMode: mode });
    },
  };
}
