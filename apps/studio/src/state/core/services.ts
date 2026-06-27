import type { ArtifactStore } from "../../domain/features/artifacts/artifacts.artifact-store.port.js";
import type { ProjectRegistry } from "../../domain/features/projects/projects.registry.port.js";
import type { StudioRuntime } from "../../domain/features/runtime/runtime.port.js";
import type { SettingsStore } from "../../domain/features/settings/settings-store.port.js";
import type { ThemeStore } from "../../domain/features/theme/theme-store.port.js";
import type { WorkspaceStore } from "../../domain/features/workspace/workspace.port.js";
import type { WorkspaceSearch } from "../../domain/features/workspace-search/index.js";
import type { TodoScanner } from "../../domain/features/todos/todos.scanner.port.js";
import type { WorkspaceWatcher } from "../../domain/features/workspace/workspace-watcher.port.js";

/**
 * Handle to every domain service the stores need. Assembled once
 * in `main.tsx` (composition root) and wired via `setServices()`
 * before any store is used — keeps stores free of `import` paths
 * into `platform/`, which the layer rules forbid.
 */
export interface StudioServices {
  readonly runtime: StudioRuntime;
  readonly artifacts: ArtifactStore;
  readonly workspace: WorkspaceStore;
  readonly workspaceSearch: WorkspaceSearch;
  readonly settings: SettingsStore;
  readonly themes: ThemeStore;
  readonly projects: ProjectRegistry;
  readonly todoScanner: TodoScanner;
  readonly workspaceWatcher: WorkspaceWatcher;
}

let current: StudioServices | null = null;

export function setServices(services: StudioServices): void {
  current = services;
}

export function getServices(): StudioServices {
  if (!current) {
    throw new Error(
      "StudioServices not wired. Call setServices() from main.tsx before any store action runs.",
    );
  }
  return current;
}

export function resetServicesForTest(): void {
  current = null;
}
