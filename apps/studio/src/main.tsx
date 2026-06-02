import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { setServices } from "./state/core/services.js";
import {
  createTheme,
  THEME_KEY,
  type ThemeApi,
} from "./state/features/theme/index.js";
import { createRuns, RUNS_KEY } from "./state/features/runs/index.js";
import {
  createSettings,
  SETTINGS_KEY,
  type SettingsApi,
} from "./state/features/settings/index.js";
import { createActions, ACTIONS_KEY } from "./state/features/actions/index.js";
import {
  createProjects,
  PROJECTS_KEY,
  type ProjectsApi,
} from "./state/features/projects/index.js";
import {
  createRunConfigs,
  RUN_CONFIGS_KEY,
} from "./state/features/run-configs/index.js";
import {
  createWorkspaceState,
  installWorkspaceStatePersistence,
  WORKSPACE_STATE_KEY,
  type WorkspaceStateApi,
} from "./state/features/workspace-state/index.js";
import {
  createWorkspace,
  WORKSPACE_KEY,
  type WorkspaceApi,
} from "./state/features/workspace/index.js";
import {
  createWorkspaceSearch,
  WORKSPACE_SEARCH_KEY,
} from "./state/features/workspace-search/index.js";
import { createPopups, POPUPS_KEY } from "./state/features/popups/index.js";
import {
  createUiInspector,
  UI_INSPECTOR_KEY,
  type UiInspectorApi,
} from "./state/features/ui-inspector/index.js";
import { createEditor, EDITOR_KEY } from "./state/features/editor/index.js";
import {
  EmbeddedRuntime,
  FsArtifactStore,
  FsProjectRegistry,
  FsSettingsStore,
  FsThemeStore,
  TauriProjectConfigStore,
  TauriTodoScanner,
  TauriWorkspaceStore,
  TauriWorkspaceSearch,
  TauriWorkspaceWatcher,
  getLaunchWorkspacePath,
} from "./platform/index.js";
import { AppShell } from "./ui/shell/app-shell.js";
import { applyTokens } from "./ui/features/theme/apply-tokens.js";
import { applyThemeMode } from "./ui/features/theme/apply-mode.js";
import { installTweaks } from "./ui/features/tweaks-panel/index.js";
import { applyMonacoTheme } from "./ui/features/theme/monaco-theme.js";
import { ensureMonacoReady } from "./ui/features/editor/monaco-init.js";
import { installKeymap } from "./ui/features/actions/keymap.js";
import { installAutoSaveOnBlur } from "./ui/features/actions/auto-save.js";
import { installFsEvents } from "./ui/features/workspace/fs-events-installer.js";
import {
  createRuntimeStatus,
  RUNTIME_STATUS_KEY,
  type RuntimeStatusApi,
} from "./state/features/runtime-status/index.js";
import { registerFeature, getFeature } from "./state/core/registry.js";
import {
  createDevices,
  DEVICES_KEY,
} from "./state/features/devices/index.js";
import { createApps, APPS_KEY } from "./state/features/apps/index.js";
import { createLayout, LAYOUT_KEY } from "./state/features/layout/index.js";
import {
  createNotifications,
  NOTIFICATIONS_KEY,
} from "./state/features/notifications/index.js";
import { createTodos, TODOS_KEY } from "./state/features/todos/index.js";
import { createProblems, PROBLEMS_KEY } from "./state/features/problems/index.js";
import {
  createMirrorWindow,
  MIRROR_WINDOW_KEY,
} from "./state/features/mirror-window/index.js";
import {
  createNavHistory,
  NAV_HISTORY_KEY,
} from "./state/features/nav-history/index.js";
import {
  createBookmarks,
  BOOKMARKS_KEY,
} from "./state/features/bookmarks/index.js";
import { createMirror, MIRROR_KEY } from "./state/features/mirror/index.js";
import {
  createProjectConfig,
  PROJECT_CONFIG_KEY,
} from "./state/features/project-config/index.js";
import { createDefaultScreenMirror } from "./platform/features/mirror/index.js";
import {
  createIosAgent,
  IOS_AGENT_KEY,
  type IosAgentApi,
  type IosAgentStatus,
} from "./state/features/ios-agent/index.js";
import { TauriIosAgentPort } from "./platform/features/ios-agent/index.js";
import {
  createAndroidAgent,
  ANDROID_AGENT_KEY,
} from "./state/features/android-agent/index.js";
import { TauriAndroidAgentPort } from "./platform/features/android-agent/index.js";
import { createLogs, LOGS_KEY, type LogsApi } from "./state/features/logs/index.js";
import { TauriLogsPort, TauriLogsSink } from "./platform/features/logs/index.js";
import type { LogsSink } from "./domain/features/logs/index.js";
import {
  createTerminal,
  TERMINAL_KEY,
} from "./state/features/terminal/index.js";
import { TauriTerminalPort } from "./platform/features/terminal/index.js";
// Side-effect imports register UI tool-windows + popups with their
// registries. Done here so registration order is explicit and does
// not depend on whatever AppShell happens to import first.
import "./ui/features/tool-windows/index.js";
import "./ui/features/workspace/index.js";
import "./ui/features/runs/index.js";
import "./ui/features/mirror/index.js";
import "./ui/features/welcome/index.js";
import "./ui/features/settings/index.js";
import "./ui/features/run-configs/index.js";
import "./ui/features/command-palette/index.js";
import "./ui/features/ui-inspector/index.js";
import "./ui/features/theme/tokens.css";
import "./ui/features/theme/app.css";
import "./ui/features/theme/tailwind.css";

/**
 * Composition root. Wires concrete platform implementations into
 * the abstract ports that Studio's stores and UI consume.
 *
 * Order matters: services are registered first, then settings
 * and theme state load, then CSS variables + Monaco theme apply,
 * then React mounts. Every store action calls `getServices()`,
 * which throws if wiring hasn't happened — the first user click
 * cannot precede this file's top-level execution, so no lock.
 */
async function bootstrap() {
  const runtime = new EmbeddedRuntime();
  const artifacts = new FsArtifactStore();
  const settings = new FsSettingsStore();
  const workspace = new TauriWorkspaceStore();
  const workspaceSearch = new TauriWorkspaceSearch();
  const projectConfigPort = new TauriProjectConfigStore();
  const themesPort = new FsThemeStore({ projectConfig: projectConfigPort });
  const projects = new FsProjectRegistry();
  const todoScanner = new TauriTodoScanner();
  const workspaceWatcher = new TauriWorkspaceWatcher();
  const screenMirror = createDefaultScreenMirror();

  setServices({
    runtime,
    artifacts,
    settings,
    workspace,
    workspaceSearch,
    themes: themesPort,
    projects,
    todoScanner,
    workspaceWatcher,
  });

  // Feature registry — register each state feature's instance
  // once. Consumers will pick them up via `useXxx()` hooks or
  // `getFeature()` accessors.
  registerFeature(WORKSPACE_KEY, createWorkspace({ store: workspace }));
  registerFeature(
    WORKSPACE_SEARCH_KEY,
    createWorkspaceSearch({ port: workspaceSearch }),
  );
  const projectConfigApi = createProjectConfig({
    port: projectConfigPort,
    getWorkspacePath: () => getFeature<WorkspaceApi>(WORKSPACE_KEY).getSnapshot().currentPath,
  });
  registerFeature(PROJECT_CONFIG_KEY, projectConfigApi);
  registerFeature(POPUPS_KEY, createPopups());
  registerFeature(UI_INSPECTOR_KEY, createUiInspector({ runtime }));
  registerFeature(
    EDITOR_KEY,
    createEditor({ workspace, getSettings: () => getFeature<SettingsApi>(SETTINGS_KEY) }),
  );
  registerFeature(DEVICES_KEY, createDevices({ runtime }));
  registerFeature(APPS_KEY, createApps({ runtime }));
  registerFeature(RUNTIME_STATUS_KEY, createRuntimeStatus({ runtime }));
  registerFeature(LAYOUT_KEY, createLayout());
  registerFeature(LOGS_KEY, createLogs());
  registerFeature(NOTIFICATIONS_KEY, createNotifications());
  registerFeature(PROBLEMS_KEY, createProblems());
  registerFeature(MIRROR_WINDOW_KEY, createMirrorWindow());
  registerFeature(NAV_HISTORY_KEY, createNavHistory());
  registerFeature(
    WORKSPACE_STATE_KEY,
    createWorkspaceState({ projectConfig: projectConfigApi }),
  );
  registerFeature(
    BOOKMARKS_KEY,
    createBookmarks({
      getPersistedBookmarks: () => {
        const s = getFeature<WorkspaceStateApi>(WORKSPACE_STATE_KEY).getSnapshot();
        if (!s.workspacePath || !s.loaded) return null;
        return s.state.bookmarks ?? [];
      },
      setPersistedBookmarks: (items) => {
        const api = getFeature<WorkspaceStateApi>(WORKSPACE_STATE_KEY);
        const s = api.getSnapshot();
        if (!s.workspacePath || !s.loaded) return;
        api.patch({ ...s.state, bookmarks: items });
      },
      subscribePersistence: (listener) =>
        getFeature<WorkspaceStateApi>(WORKSPACE_STATE_KEY).subscribe(() => {
          const s = getFeature<WorkspaceStateApi>(WORKSPACE_STATE_KEY).getSnapshot();
          listener(s.loaded ? s.state.bookmarks ?? [] : null);
        }),
    }),
  );
  registerFeature(
    TODOS_KEY,
    createTodos({
      scanner: todoScanner,
      getWorkspacePath: () => getFeature<WorkspaceApi>(WORKSPACE_KEY).getSnapshot().currentPath,
    }),
  );
  registerFeature(SETTINGS_KEY, createSettings({ store: settings }));
  registerFeature(ACTIONS_KEY, createActions());
  registerFeature(THEME_KEY, createTheme({ store: themesPort }));
  registerFeature(RUNS_KEY, createRuns({ runtime, artifacts }));
  registerFeature(PROJECTS_KEY, createProjects({ registry: projects }));
  registerFeature(
    MIRROR_KEY,
    createMirror({
      port: screenMirror,
      onInteraction: () => getFeature<UiInspectorApi>(UI_INSPECTOR_KEY).notifyInteraction(),
    }),
  );
  registerFeature(
    IOS_AGENT_KEY,
    createIosAgent({ port: new TauriIosAgentPort() }),
  );
  registerFeature(
    ANDROID_AGENT_KEY,
    createAndroidAgent({ port: new TauriAndroidAgentPort() }),
  );
  registerFeature(
    TERMINAL_KEY,
    createTerminal({ port: new TauriTerminalPort() }),
  );
  registerFeature(
    RUN_CONFIGS_KEY,
    createRunConfigs({
      projectConfig: projectConfigApi,
      getLastActiveRunConfig: () =>
        getFeature<WorkspaceStateApi>(WORKSPACE_STATE_KEY).getSnapshot().state.lastActiveRunConfig,
      setLastActiveRunConfig: (id) => {
        const api = getFeature<WorkspaceStateApi>(WORKSPACE_STATE_KEY);
        const s = api.getSnapshot();
        api.patch({ ...s.state, lastActiveRunConfig: id });
      },
    }),
  );

  await runtime.connect();
  await getFeature<SettingsApi>(SETTINGS_KEY).load();
  await getFeature<ProjectsApi>(PROJECTS_KEY).reload();
  const persisted = getFeature<SettingsApi>(SETTINGS_KEY).getSnapshot().settings;

  // Single source of truth: `StudioSettings.inspectorAutoRefresh`
  // is the persisted store; the inspector reflects it. The
  // settings dialog only patches the settings record — this
  // subscription propagates each change into the inspector
  // without requiring callers to update both.
  const applyInspectorAutoRefresh = (): void => {
    const cfg = getFeature<SettingsApi>(SETTINGS_KEY).getSnapshot().settings.inspectorAutoRefresh;
    getFeature<UiInspectorApi>(UI_INSPECTOR_KEY).setAutoRefreshInterval(cfg.intervalMs);
    getFeature<UiInspectorApi>(UI_INSPECTOR_KEY).setAutoRefreshEnabled(cfg.enabled);
  };
  applyInspectorAutoRefresh();
  getFeature<SettingsApi>(SETTINGS_KEY).subscribe(applyInspectorAutoRefresh);
  const ts = getFeature<ThemeApi>(THEME_KEY);
  if (persisted.editorThemeId) {
    await ts.reload();
    await ts.setActiveId(persisted.editorThemeId);
  } else {
    await ts.reload();
  }

  ensureMonacoReady();

  // Install the fs watcher: reload themes whenever the user
  // themes folder changes. The reload is debounced by how often
  // zustand de-dupes equal effective maps.
  themesPort.watch(async () => {
    const path = getFeature<WorkspaceApi>(WORKSPACE_KEY).getSnapshot().currentPath;
    await getFeature<ThemeApi>(THEME_KEY).reload(path ?? undefined);
  });

  installWorkspaceStatePersistence();

  // Startup behavior:
  //   1. If this window was spawned for a specific workspace (query
  //      param), open it — that's the per-window isolation model.
  //   2. Otherwise honour `settings.startupBehavior`. `reopenLast`
  //      (default) jumps straight into the most recent project;
  //      `showWelcome` falls through to the Welcome screen.
  const launchPath = getLaunchWorkspacePath();
  if (launchPath) {
    void getFeature<WorkspaceApi>(WORKSPACE_KEY).openFolder(launchPath);
  } else {
    const startupBehavior =
      getFeature<SettingsApi>(SETTINGS_KEY).getSnapshot().settings.startupBehavior ?? "reopenLast";
    if (startupBehavior === "reopenLast") {
      const mostRecent = getFeature<ProjectsApi>(PROJECTS_KEY).getSnapshot().items[0];
      if (mostRecent) {
        void getFeature<WorkspaceApi>(WORKSPACE_KEY).openFolder(mostRecent.path);
      }
    }
  }

  const initialTheme = getFeature<ThemeApi>(THEME_KEY).getSnapshot();
  applyTokens(initialTheme.effective);
  if (initialTheme.activeId) {
    const active = initialTheme.library.get(initialTheme.activeId);
    if (active) {
      applyThemeMode(active);
      applyMonacoTheme(initialTheme.effective, active.monacoBase);
    }
  }
  installTweaks();
  let previousEffective = initialTheme.effective;
  let previousActiveId = initialTheme.activeId;
  getFeature<ThemeApi>(THEME_KEY).subscribe(() => {
    const state = getFeature<ThemeApi>(THEME_KEY).getSnapshot();
    if (state.effective !== previousEffective) {
      previousEffective = state.effective;
      applyTokens(state.effective);
      if (state.activeId) {
        const active = state.library.get(state.activeId);
        if (active) applyMonacoTheme(state.effective, active.monacoBase);
      }
    }
    if (state.activeId !== previousActiveId) {
      previousActiveId = state.activeId;
      if (state.activeId) {
        const active = state.library.get(state.activeId);
        if (active) applyThemeMode(active);
      }
    }
  });

  installKeymap();
  installAutoSaveOnBlur();
  installFsEvents({ watcher: workspaceWatcher, workspaceStore: workspace });
  installLogsBridge();
  installIosAgentLogsBridge();
  installConsoleProxy(new TauriLogsSink());

  // Periodic runtime ping — reflects sidecar connectivity in the
  // toolbar indicator. 10s cadence is coarse enough to be cheap
  // and fine enough to catch crashes before the next user action.
  void getFeature<RuntimeStatusApi>(RUNTIME_STATUS_KEY).ping();
  setInterval(() => void getFeature<RuntimeStatusApi>(RUNTIME_STATUS_KEY).ping(), 10_000);

  const root = document.getElementById("root");
  if (!root) throw new Error("#root element missing from index.html");

  createRoot(root).render(
    <StrictMode>
      <AppShell />
    </StrictMode>,
  );
}

function installLogsBridge() {
  const port = new TauriLogsPort();
  port.subscribe((entry) => {
    getFeature<LogsApi>(LOGS_KEY).append(entry);
  });
}

/**
 * Fan out iOS agent state transitions into the Logs tool window.
 * Each udid's status is polled by consumers that need it; whenever
 * state changes (building → ready, ready → failed, etc.) a log
 * entry surfaces with the message the sidecar attached — including
 * XCUITest launch errors, signing failures, and simulator boot
 * issues. The bridge runs once at composition time and keeps
 * working for the lifetime of the window.
 */
function installIosAgentLogsBridge() {
  const api = getFeature<IosAgentApi>(IOS_AGENT_KEY);
  const logs = getFeature<LogsApi>(LOGS_KEY);
  const prev = new Map<string, IosAgentStatus>();
  api.subscribe(() => {
    const snapshot = api.getSnapshot();
    for (const [udid, status] of Object.entries(snapshot.byUdid)) {
      const before = prev.get(udid);
      if (before && before.state === status.state && before.message === status.message) {
        continue;
      }
      prev.set(udid, status);
      const level =
        status.state === "failed"
          ? "error"
          : status.state === "ready"
            ? "info"
            : status.state === "building"
              ? "info"
              : "debug";
      const head = `[${udid.slice(0, 8)}] ${status.state}`;
      const message = status.message ? `${head} — ${status.message}` : head;
      logs.append({
        id: Math.random().toString(36).slice(2),
        ts: Date.now(),
        source: "ios-agent",
        level,
        message,
      });
    }
  });
}

function installConsoleProxy(sink: LogsSink) {
  const original = {
    log: console.log.bind(console),
    info: console.info.bind(console),
    warn: console.warn.bind(console),
    error: console.error.bind(console),
    debug: console.debug.bind(console),
  };
  const forward = (level: "debug" | "info" | "warn" | "error") =>
    (...args: unknown[]) => {
      original[level === "debug" ? "debug" : level](...args);
      const message = args
        .map((a) =>
          typeof a === "string"
            ? a
            : a instanceof Error
              ? a.stack ?? a.message
              : safeStringify(a),
        )
        .join(" ");
      sink.emit({ source: "ui", level, message });
    };
  console.log = forward("info");
  console.info = forward("info");
  console.warn = forward("warn");
  console.error = forward("error");
  console.debug = forward("debug");
}

function safeStringify(v: unknown): string {
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
}

void bootstrap();
