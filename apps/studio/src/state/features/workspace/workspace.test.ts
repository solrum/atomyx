import { describe, it, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { MockWorkspaceStore } from "../../../domain/features/workspace/workspace.mock.js";
import { MockRuntime } from "../../../domain/features/runtime/runtime.mock.js";
import { MockArtifactStore } from "../../../domain/features/artifacts/artifact-store.mock.js";
import { MockSettingsStore } from "../../../domain/features/settings/settings-store.mock.js";
import { MockThemeStore } from "../../../domain/features/theme/theme-store.mock.js";
import { MockProjectRegistry } from "../../../domain/features/projects/registry.mock.js";
import { MockProjectConfigStore } from "../../../domain/features/project-config/index.js";
import { MockTodoScanner } from "../../../domain/features/todos/scanner.mock.js";
import { MockWorkspaceWatcher } from "../../../domain/features/workspace/workspace-watcher.mock.js";
import { resetServicesForTest, setServices } from "../../core/services.js";
import { registerFeature, resetFeaturesForTest } from "../../core/registry.js";
import { createLayout, LAYOUT_KEY } from "../layout/index.js";
import {
  createNotifications,
  NOTIFICATIONS_KEY,
} from "../notifications/index.js";
import { createProjects, PROJECTS_KEY } from "../projects/index.js";
import {
  createProjectConfig,
  PROJECT_CONFIG_KEY,
} from "../project-config/index.js";
import { createRunConfigs, RUN_CONFIGS_KEY } from "../run-configs/index.js";
import { createTheme, THEME_KEY } from "../theme/index.js";
import {
  createWorkspaceState,
  WORKSPACE_STATE_KEY,
} from "../workspace-state/index.js";
import {
  createWorkspace,
  WORKSPACE_KEY,
  type WorkspaceApi,
} from "../workspace/index.js";
import { getFeature } from "../../core/registry.js";
import { createEditor, EDITOR_KEY } from "../editor/index.js";
import { createSettings, SETTINGS_KEY } from "../settings/index.js";

describe("workspace feature", () => {
  beforeEach(() => {
    resetServicesForTest();
    resetFeaturesForTest();
    registerFeature(LAYOUT_KEY, createLayout());
    registerFeature(NOTIFICATIONS_KEY, createNotifications());
  });

  it("loads a folder through the workspace port", async () => {
    const projectsPort = new MockProjectRegistry();
    const projectConfigPort = new MockProjectConfigStore();
    const themesPort = new MockThemeStore();
    const settingsPort = new MockSettingsStore();
    const workspacePort = new MockWorkspaceStore({
      trees: {
        "/root": {
          rootPath: "/root",
          entries: [{ path: "/root/a.yml", name: "a.yml", type: "file" }],
        },
      },
    });
    setServices({
      runtime: new MockRuntime(),
      artifacts: new MockArtifactStore(),
      settings: settingsPort,
      themes: themesPort,
      projects: projectsPort,
      todoScanner: new MockTodoScanner(),
      workspaceWatcher: new MockWorkspaceWatcher(),
      workspace: workspacePort,
    });
    registerFeature(WORKSPACE_KEY, createWorkspace({ store: workspacePort }));
    const settingsApi = createSettings({ store: settingsPort });
    registerFeature(SETTINGS_KEY, settingsApi);
    registerFeature(
      EDITOR_KEY,
      createEditor({
        workspace: workspacePort,
        getSettings: () => settingsApi,
      }),
    );
    registerFeature(PROJECTS_KEY, createProjects({ registry: projectsPort }));
    registerFeature(THEME_KEY, createTheme({ store: themesPort }));
    const projectConfigApi = createProjectConfig({
      port: projectConfigPort,
      getWorkspacePath: () => getFeature<WorkspaceApi>(WORKSPACE_KEY).getSnapshot().currentPath,
    });
    registerFeature(PROJECT_CONFIG_KEY, projectConfigApi);
    registerFeature(
      WORKSPACE_STATE_KEY,
      createWorkspaceState({ projectConfig: projectConfigApi }),
    );
    registerFeature(
      RUN_CONFIGS_KEY,
      createRunConfigs({
        projectConfig: projectConfigApi,
        getLastActiveRunConfig: () => null,
        setLastActiveRunConfig: () => {},
      }),
    );
    await getFeature<WorkspaceApi>(WORKSPACE_KEY).openFolder("/root");
    const state = getFeature<WorkspaceApi>(WORKSPACE_KEY).getSnapshot();
    assert.equal(state.currentPath, "/root");
    assert.equal(state.tree?.entries.length, 1);
    assert.equal(state.error, null);
  });
});
