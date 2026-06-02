import { getFeature } from "../../../state/core/registry.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";
import type { NotificationsApi } from "../../../state/features/notifications/index.js";
import { NOTIFICATIONS_KEY } from "../../../state/features/notifications/index.js";
import type { WorkspaceApi } from "../../../state/features/workspace/index.js";
import { WORKSPACE_KEY } from "../../../state/features/workspace/index.js";
import type {
  FsChangeEvent,
  WorkspaceStore,
  WorkspaceWatcher,
} from "../../../domain/features/workspace/index.js";

const TREE_RELOAD_DEBOUNCE_MS = 150;
const STALE_PROMPT_TTL_MS = 30_000;

export interface FsEventsDeps {
  readonly watcher: WorkspaceWatcher;
  readonly workspaceStore: WorkspaceStore;
}

export function installFsEvents(deps: FsEventsDeps): void {
  let treeReloadTimer: number | null = null;
  let currentWatchedPath: string | null = null;
  let previousPath: string | null = getFeature<WorkspaceApi>(WORKSPACE_KEY).getSnapshot().currentPath;

  getFeature<WorkspaceApi>(WORKSPACE_KEY).subscribe(() => {
    const current = getFeature<WorkspaceApi>(WORKSPACE_KEY).getSnapshot().currentPath;
    if (current === previousPath) return;
    previousPath = current;
    if (currentWatchedPath !== null && current !== currentWatchedPath) {
      void deps.watcher.stop().catch(() => {});
      currentWatchedPath = null;
    }
    if (current && current !== currentWatchedPath) {
      const path = current;
      currentWatchedPath = path;
      void deps.watcher
        .start(path, (event) => handleEvent(deps, event, () => {
          if (treeReloadTimer !== null) window.clearTimeout(treeReloadTimer);
          treeReloadTimer = window.setTimeout(() => {
            treeReloadTimer = null;
            void getFeature<WorkspaceApi>(WORKSPACE_KEY).reloadTree();
          }, TREE_RELOAD_DEBOUNCE_MS);
        }))
        .catch(() => {
          currentWatchedPath = null;
        });
    }
  });
}

function handleEvent(
  deps: FsEventsDeps,
  event: FsChangeEvent,
  scheduleTreeReload: () => void,
): void {
  scheduleTreeReload();
  for (const path of event.paths) {
    reconcileOpenTab(deps, path, event.kind);
  }
}

function reconcileOpenTab(
  deps: FsEventsDeps,
  path: string,
  kind: FsChangeEvent["kind"],
): void {
  const tab = getFeature<EditorApi>(EDITOR_KEY).getSnapshot().tabs.find((t) => t.path === path);
  if (!tab) return;
  if (kind === "removed") {
    getFeature<NotificationsApi>(NOTIFICATIONS_KEY).show({
      kind: "warn",
      title: "File removed externally",
      detail: `${baseName(path)} no longer exists on disk.`,
    });
    return;
  }
  if (kind !== "modified" && kind !== "created") return;
  void applyExternalChange(deps, path);
}

async function applyExternalChange(
  deps: FsEventsDeps,
  path: string,
): Promise<void> {
  let contents: string;
  try {
    contents = await deps.workspaceStore.readScript(path);
  } catch {
    return;
  }
  const tab = getFeature<EditorApi>(EDITOR_KEY).getSnapshot().tabs.find((t) => t.path === path);
  if (!tab) return;
  if (contents === tab.content) return;
  if (!tab.dirty) {
    getFeature<EditorApi>(EDITOR_KEY).reloadFromDisk(path, contents);
    return;
  }
  const id = getFeature<NotificationsApi>(NOTIFICATIONS_KEY).show({
    kind: "warn",
    title: "File changed on disk",
    detail: `${baseName(path)} has unsaved local changes. Save or discard to adopt the external edit.`,
    pinned: true,
  });
  const timer = window.setTimeout(() => {
    getFeature<NotificationsApi>(NOTIFICATIONS_KEY).dismiss(id);
  }, STALE_PROMPT_TTL_MS);
  const unsub = getFeature<EditorApi>(EDITOR_KEY).subscribe(() => {
    const t = getFeature<EditorApi>(EDITOR_KEY)
      .getSnapshot()
      .tabs.find((x) => x.path === path);
    if (!t) {
      window.clearTimeout(timer);
      getFeature<NotificationsApi>(NOTIFICATIONS_KEY).dismiss(id);
      unsub();
      return;
    }
    if (!t.dirty) {
      if (t.content !== contents) {
        getFeature<EditorApi>(EDITOR_KEY).reloadFromDisk(path, contents);
      }
      window.clearTimeout(timer);
      getFeature<NotificationsApi>(NOTIFICATIONS_KEY).dismiss(id);
      unsub();
    }
  });
}

function baseName(path: string): string {
  return path.split("/").pop() ?? path;
}
