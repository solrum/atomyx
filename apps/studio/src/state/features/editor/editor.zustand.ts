import { createStore } from "zustand/vanilla";
import type { WorkspaceStore as WorkspacePort } from "../../../domain/features/workspace/index.js";
import type { SettingsApi } from "../settings/index.js";
import type {
  EditorApi,
  EditorGroup,
  EditorSnapshot,
  EditorTab,
} from "./editor.contract.js";

export interface EditorDeps {
  readonly workspace: WorkspacePort;
  readonly getSettings: () => SettingsApi;
}

const CLOSED_STACK_CAP = 20;
const PRIMARY_GROUP_ID = "g1";

function makeGroupId(): string {
  return `g${Math.random().toString(36).slice(2, 8)}`;
}

function replaceTab(
  tabs: readonly EditorTab[],
  path: string,
  patch: Partial<EditorTab>,
): readonly EditorTab[] {
  return tabs.map((tab) => (tab.path === path ? { ...tab, ...patch } : tab));
}

function deriveMirror(
  groups: readonly EditorGroup[],
  activeGroupId: string,
): { tabs: readonly EditorTab[]; activePath: string | null } {
  const group = groups.find((g) => g.id === activeGroupId) ?? groups[0];
  return {
    tabs: group?.tabs ?? [],
    activePath: group?.activePath ?? null,
  };
}

export function createZustandEditor(deps: EditorDeps): EditorApi {
  const { workspace, getSettings } = deps;

  function stripTrailing(content: string): string {
    return content
      .split("\n")
      .map((line) => line.replace(/[ \t]+$/g, ""))
      .join("\n");
  }

  function applySavePreprocessing(content: string): string {
    return getSettings().getSnapshot().settings.stripTrailingWhitespaceOnSave
      ? stripTrailing(content)
      : content;
  }

  const initialGroup: EditorGroup = {
    id: PRIMARY_GROUP_ID,
    tabs: [],
    activePath: null,
  };

  const store = createStore<EditorSnapshot>(() => ({
    groups: [initialGroup],
    activeGroupId: PRIMARY_GROUP_ID,
    tabs: [],
    activePath: null,
    closedStack: [],
  }));

  function mutateActive(patch: (group: EditorGroup) => EditorGroup): void {
    const state = store.getState();
    const groups = state.groups.map((g) =>
      g.id === state.activeGroupId ? patch(g) : g,
    );
    store.setState({
      groups,
      ...deriveMirror(groups, state.activeGroupId),
    });
  }

  const api: EditorApi = {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    async openFile(path) {
      const state = store.getState();
      const activeGroup = state.groups.find((g) => g.id === state.activeGroupId);
      if (!activeGroup) return;
      if (activeGroup.tabs.some((tab) => tab.path === path)) {
        mutateActive((g) => ({ ...g, activePath: path }));
        return;
      }
      const content = await workspace.readScript(path);
      mutateActive((g) => ({
        ...g,
        tabs: [
          ...g.tabs,
          {
            path,
            content,
            savedContent: content,
            dirty: false,
            pinned: false,
          },
        ],
        activePath: path,
      }));
    },

    closeFile(path) {
      const state = store.getState();
      const activeGroup = state.groups.find((g) => g.id === state.activeGroupId);
      if (!activeGroup) return;
      const tab = activeGroup.tabs.find((t) => t.path === path);
      if (tab?.pinned) return;
      const tabs = activeGroup.tabs.filter((t) => t.path !== path);
      const activePath =
        activeGroup.activePath === path
          ? (tabs[tabs.length - 1]?.path ?? null)
          : activeGroup.activePath;
      const prevClosed = state.closedStack.filter((p) => p !== path);
      const closedStack = [...prevClosed, path].slice(-CLOSED_STACK_CAP);
      const groups = state.groups.map((g) =>
        g.id === activeGroup.id ? { ...g, tabs, activePath } : g,
      );
      store.setState({
        groups,
        closedStack,
        ...deriveMirror(groups, state.activeGroupId),
      });
    },

    closeOthers(keepPath) {
      const state = store.getState();
      const activeGroup = state.groups.find((g) => g.id === state.activeGroupId);
      if (!activeGroup) return;
      const keep = activeGroup.tabs.filter(
        (t) => t.path === keepPath || t.pinned,
      );
      const closed = activeGroup.tabs
        .filter((t) => t.path !== keepPath && !t.pinned)
        .map((t) => t.path);
      const prev = state.closedStack.filter((p) => !closed.includes(p));
      const groups = state.groups.map((g) =>
        g.id === activeGroup.id
          ? { ...g, tabs: keep, activePath: keepPath }
          : g,
      );
      store.setState({
        groups,
        closedStack: [...prev, ...closed].slice(-CLOSED_STACK_CAP),
        ...deriveMirror(groups, state.activeGroupId),
      });
    },

    closeToRight(keepPath) {
      const state = store.getState();
      const activeGroup = state.groups.find((g) => g.id === state.activeGroupId);
      if (!activeGroup) return;
      const idx = activeGroup.tabs.findIndex((t) => t.path === keepPath);
      if (idx < 0) return;
      const left = activeGroup.tabs.slice(0, idx + 1);
      const right = activeGroup.tabs.slice(idx + 1);
      const keepRight = right.filter((t) => t.pinned);
      const closed = right.filter((t) => !t.pinned).map((t) => t.path);
      const prev = state.closedStack.filter((p) => !closed.includes(p));
      const activePath = closed.includes(activeGroup.activePath ?? "")
        ? keepPath
        : activeGroup.activePath;
      const groups = state.groups.map((g) =>
        g.id === activeGroup.id
          ? { ...g, tabs: [...left, ...keepRight], activePath }
          : g,
      );
      store.setState({
        groups,
        closedStack: [...prev, ...closed].slice(-CLOSED_STACK_CAP),
        ...deriveMirror(groups, state.activeGroupId),
      });
    },

    closeAll() {
      const state = store.getState();
      const activeGroup = state.groups.find((g) => g.id === state.activeGroupId);
      if (!activeGroup) return;
      const keep = activeGroup.tabs.filter((t) => t.pinned);
      const closed = activeGroup.tabs
        .filter((t) => !t.pinned)
        .map((t) => t.path);
      const prev = state.closedStack.filter((p) => !closed.includes(p));
      const activePath =
        keep.find((t) => t.path === activeGroup.activePath)?.path ??
        keep[keep.length - 1]?.path ??
        null;
      const groups = state.groups.map((g) =>
        g.id === activeGroup.id ? { ...g, tabs: keep, activePath } : g,
      );
      store.setState({
        groups,
        closedStack: [...prev, ...closed].slice(-CLOSED_STACK_CAP),
        ...deriveMirror(groups, state.activeGroupId),
      });
    },

    activate(path) {
      mutateActive((g) => {
        if (!g.tabs.some((t) => t.path === path)) return g;
        return { ...g, activePath: path };
      });
    },

    updateContent(path, content) {
      mutateActive((g) => {
        const tab = g.tabs.find((t) => t.path === path);
        if (!tab) return g;
        return {
          ...g,
          tabs: replaceTab(g.tabs, path, {
            content,
            dirty: content !== tab.savedContent,
          }),
        };
      });
    },

    async saveActive() {
      const state = store.getState();
      const activeGroup = state.groups.find((g) => g.id === state.activeGroupId);
      if (!activeGroup) return;
      const active = activeGroup.tabs.find((t) => t.path === activeGroup.activePath);
      if (!active) return;
      const toWrite = applySavePreprocessing(active.content);
      await workspace.writeScript(active.path, toWrite);
      mutateActive((g) => ({
        ...g,
        tabs: replaceTab(g.tabs, active.path, {
          content: toWrite,
          savedContent: toWrite,
          dirty: false,
        }),
      }));
    },

    async saveAll() {
      const state = store.getState();
      const dirtyPaths = new Set<string>();
      for (const g of state.groups) {
        for (const t of g.tabs) if (t.dirty) dirtyPaths.add(t.path);
      }
      const writes: { path: string; content: string }[] = [];
      for (const path of dirtyPaths) {
        const tab = state.groups
          .flatMap((g) => g.tabs)
          .find((t) => t.path === path);
        if (tab) {
          writes.push({ path, content: applySavePreprocessing(tab.content) });
        }
      }
      for (const w of writes) {
        await workspace.writeScript(w.path, w.content);
      }
      const saved = new Map(writes.map((w) => [w.path, w.content] as const));
      const groups = state.groups.map((g) => ({
        ...g,
        tabs: g.tabs.map((tab) => {
          const s = saved.get(tab.path);
          return s !== undefined
            ? { ...tab, content: s, savedContent: s, dirty: false }
            : tab;
        }),
      }));
      store.setState({
        groups,
        ...deriveMirror(groups, state.activeGroupId),
      });
    },

    async reopenLastClosed() {
      const stack = store.getState().closedStack;
      if (stack.length === 0) return;
      const last = stack[stack.length - 1]!;
      store.setState({ closedStack: stack.slice(0, -1) });
      await api.openFile(last);
    },

    renameTab(oldPath, newPath) {
      const state = store.getState();
      const groups = state.groups.map((g) => ({
        ...g,
        tabs: g.tabs.map((tab) =>
          tab.path === oldPath ? { ...tab, path: newPath } : tab,
        ),
        activePath: g.activePath === oldPath ? newPath : g.activePath,
      }));
      store.setState({
        groups,
        ...deriveMirror(groups, state.activeGroupId),
      });
    },

    nextTab() {
      mutateActive((g) => {
        if (g.tabs.length < 2) return g;
        const idx = g.tabs.findIndex((t) => t.path === g.activePath);
        const next = g.tabs[(idx + 1) % g.tabs.length];
        return next ? { ...g, activePath: next.path } : g;
      });
    },

    previousTab() {
      mutateActive((g) => {
        if (g.tabs.length < 2) return g;
        const idx = g.tabs.findIndex((t) => t.path === g.activePath);
        const prev = g.tabs[(idx - 1 + g.tabs.length) % g.tabs.length];
        return prev ? { ...g, activePath: prev.path } : g;
      });
    },

    togglePinned(path) {
      mutateActive((g) => ({
        ...g,
        tabs: g.tabs.map((tab) =>
          tab.path === path ? { ...tab, pinned: !tab.pinned } : tab,
        ),
      }));
    },

    reloadFromDisk(path, contents) {
      const state = store.getState();
      const groups = state.groups.map((g) => ({
        ...g,
        tabs: g.tabs.map((tab) =>
          tab.path === path
            ? { ...tab, content: contents, savedContent: contents, dirty: false }
            : tab,
        ),
      }));
      store.setState({
        groups,
        ...deriveMirror(groups, state.activeGroupId),
      });
    },

    splitRight() {
      const state = store.getState();
      const activeGroup = state.groups.find((g) => g.id === state.activeGroupId);
      if (!activeGroup || state.groups.length >= 2) {
        return;
      }
      const activeTab = activeGroup.tabs.find(
        (t) => t.path === activeGroup.activePath,
      );
      if (!activeTab) return;
      const newGroupId = makeGroupId();
      const newGroup: EditorGroup = {
        id: newGroupId,
        tabs: [{ ...activeTab }],
        activePath: activeTab.path,
      };
      const groups = [...state.groups, newGroup];
      store.setState({
        groups,
        activeGroupId: newGroupId,
        ...deriveMirror(groups, newGroupId),
      });
    },

    closeGroup(groupId) {
      const state = store.getState();
      if (state.groups.length <= 1) return;
      const groups = state.groups.filter((g) => g.id !== groupId);
      const activeGroupId =
        state.activeGroupId === groupId
          ? (groups[0]?.id ?? PRIMARY_GROUP_ID)
          : state.activeGroupId;
      store.setState({
        groups,
        activeGroupId,
        ...deriveMirror(groups, activeGroupId),
      });
    },

    focusGroup(groupId) {
      const state = store.getState();
      if (!state.groups.some((g) => g.id === groupId)) return;
      store.setState({
        activeGroupId: groupId,
        ...deriveMirror(state.groups, groupId),
      });
    },
  };

  return api;
}
