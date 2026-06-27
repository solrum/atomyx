export type BottomPane =
  | "problems"
  | "todos"
  | "terminal"
  | "history"
  | "logs"
  | "scenario";

export type ViewMode = "author" | "live" | "debug";

export interface PaneSizes {
  readonly fileTreeWidth: number;
  readonly mirrorWidth: number;
  readonly runDrawerHeight: number;
  readonly inspectorWidth: number;
  readonly bottomPaneHeight: number;
}

export const DEFAULT_PANE_SIZES: PaneSizes = {
  fileTreeWidth: 256,
  mirrorWidth: 320,
  runDrawerHeight: 240,
  inspectorWidth: 360,
  bottomPaneHeight: 192,
};

export interface LayoutSnapshot {
  readonly fileTreeVisible: boolean;
  readonly runDrawerVisible: boolean;
  readonly runDrawerCollapsed: boolean;
  readonly inspectorVisible: boolean;
  readonly structureVisible: boolean;
  readonly problemsVisible: boolean;
  readonly bottomPane: BottomPane;
  readonly zenMode: boolean;
  readonly settingsViewVisible: boolean;
  readonly viewMode: ViewMode;
  readonly paneSizes: PaneSizes;
}

export interface LayoutApi {
  getSnapshot(): LayoutSnapshot;
  subscribe(listener: () => void): () => void;

  toggleFileTree(): void;
  toggleRunDrawer(): void;
  toggleRunDrawerCollapsed(): void;
  toggleInspector(): void;
  toggleStructure(): void;
  toggleProblems(): void;
  toggleTodos(): void;
  toggleTerminal(): void;
  toggleHistory(): void;
  toggleLogs(): void;
  toggleScenario(): void;
  toggleZen(): void;
  toggleSettingsView(): void;
  setSettingsView(visible: boolean): void;

  setFileTree(visible: boolean): void;
  setRunDrawer(visible: boolean): void;
  setRunDrawerCollapsed(collapsed: boolean): void;
  setInspector(visible: boolean): void;
  setProblems(visible: boolean): void;
  setBottomPane(pane: BottomPane): void;

  setPaneSize(key: keyof PaneSizes, value: number): void;
  resetPaneSize(key: keyof PaneSizes): void;

  setViewMode(mode: ViewMode): void;
}
