import assert from "node:assert/strict";
import { describe, it, beforeEach } from "node:test";
import { createZustandEditor } from "./editor.zustand.js";
import type { EditorApi } from "./editor.contract.js";
import type { WorkspaceStore as WorkspacePort } from "../../../domain/features/workspace/index.js";
import type { SettingsApi } from "../settings/index.js";
import type { SettingsSnapshot } from "../settings/settings.contract.js";

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

function makeWorkspace(files: Record<string, string> = {}): WorkspacePort {
  const store: Record<string, string> = { ...files };
  return {
    async readScript(path) {
      if (!(path in store)) throw new Error(`File not found: ${path}`);
      return store[path]!;
    },
    async writeScript(path, content) {
      store[path] = content;
    },
    async openFolder() {
      return { rootPath: "/", entries: [] };
    },
    async createScript() {
      return "/new";
    },
    async createFolder() {
      return "/new-folder";
    },
    async deleteScript() {},
    async renameScript(path) {
      return path;
    },
    async pickFolder() {
      return null;
    },
  };
}

function makeSettings(strip = true): SettingsApi {
  const snap: SettingsSnapshot = {
    settings: {
      editorThemeId: "default",
      startupBehavior: "showWelcome",
      themeOverrides: {},
      useBundledFont: true,
      artifactRetention: { maxRuns: 50, maxSizeMB: 500 },
      mcp: { mode: "embedded", endpoint: null },
      autoUpdate: { enabled: false, channel: "stable", endpoint: null },
      artifactsLocation: "app-data",
      fileFilter: { extensions: [".yml"] },
      stripTrailingWhitespaceOnSave: strip,
      autoSaveOnBlur: false,
      inspectorAutoRefresh: { enabled: false, intervalMs: 5000 },
    },
    loaded: true,
  };
  return {
    getSnapshot: () => snap,
    subscribe: () => () => {},
    async load() {},
    async update() {},
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeEditor(
  files: Record<string, string> = {},
  strip = false,
): EditorApi {
  return createZustandEditor({
    workspace: makeWorkspace(files),
    getSettings: () => makeSettings(strip),
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("editor.zustand — initial snapshot", () => {
  it("starts with no tabs and no active path", () => {
    const editor = makeEditor();
    const snap = editor.getSnapshot();
    assert.equal(snap.tabs.length, 0);
    assert.equal(snap.activePath, null);
    assert.equal(snap.closedStack.length, 0);
    assert.equal(snap.groups.length, 1);
  });
});

describe("editor.zustand — openFile", () => {
  let editor: EditorApi;

  beforeEach(() => {
    editor = makeEditor({ "/a.yml": "a: 1", "/b.yml": "b: 2" });
  });

  it("opens a file and sets it as active", async () => {
    await editor.openFile("/a.yml");
    const snap = editor.getSnapshot();
    assert.equal(snap.tabs.length, 1);
    assert.equal(snap.activePath, "/a.yml");
    assert.equal(snap.tabs[0]!.content, "a: 1");
    assert.equal(snap.tabs[0]!.dirty, false);
    assert.equal(snap.tabs[0]!.pinned, false);
  });

  it("opens multiple files appending tabs", async () => {
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    const snap = editor.getSnapshot();
    assert.equal(snap.tabs.length, 2);
    assert.equal(snap.activePath, "/b.yml");
  });

  it("activates without duplicating when opening an already-open file", async () => {
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    await editor.openFile("/a.yml");
    const snap = editor.getSnapshot();
    assert.equal(snap.tabs.length, 2);
    assert.equal(snap.activePath, "/a.yml");
  });
});

describe("editor.zustand — activate", () => {
  it("switches the active tab", async () => {
    const editor = makeEditor({ "/a.yml": "", "/b.yml": "" });
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    editor.activate("/a.yml");
    assert.equal(editor.getSnapshot().activePath, "/a.yml");
  });

  it("is a no-op for an unknown path", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.activate("/unknown.yml");
    assert.equal(editor.getSnapshot().activePath, "/a.yml");
  });
});

describe("editor.zustand — closeFile", () => {
  let editor: EditorApi;

  beforeEach(async () => {
    editor = makeEditor({ "/a.yml": "", "/b.yml": "", "/c.yml": "" });
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    await editor.openFile("/c.yml");
  });

  it("removes a non-active tab without changing active path", () => {
    editor.activate("/b.yml");
    editor.closeFile("/a.yml");
    const snap = editor.getSnapshot();
    assert.equal(snap.tabs.length, 2);
    assert.equal(snap.activePath, "/b.yml");
    assert.ok(!snap.tabs.some((t) => t.path === "/a.yml"));
  });

  it("closes the active tab and falls back to the last remaining tab", () => {
    editor.activate("/c.yml");
    editor.closeFile("/c.yml");
    const snap = editor.getSnapshot();
    assert.equal(snap.tabs.length, 2);
    assert.equal(snap.activePath, "/b.yml");
  });

  it("closes the last tab resulting in null activePath", async () => {
    const single = makeEditor({ "/only.yml": "" });
    await single.openFile("/only.yml");
    single.closeFile("/only.yml");
    const snap = single.getSnapshot();
    assert.equal(snap.tabs.length, 0);
    assert.equal(snap.activePath, null);
  });

  it("adds closed path to closedStack", () => {
    editor.closeFile("/a.yml");
    assert.ok(editor.getSnapshot().closedStack.includes("/a.yml"));
  });

  it("does not close a pinned tab", async () => {
    editor.togglePinned("/a.yml");
    editor.closeFile("/a.yml");
    assert.equal(editor.getSnapshot().tabs.length, 3);
  });
});

describe("editor.zustand — closeOthers", () => {
  it("keeps only the specified tab (and pinned tabs)", async () => {
    const editor = makeEditor({ "/a.yml": "", "/b.yml": "", "/c.yml": "" });
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    await editor.openFile("/c.yml");
    editor.togglePinned("/a.yml");
    editor.closeOthers("/b.yml");
    const snap = editor.getSnapshot();
    assert.equal(snap.tabs.length, 2);
    assert.ok(snap.tabs.some((t) => t.path === "/b.yml"));
    assert.ok(snap.tabs.some((t) => t.path === "/a.yml"));
    assert.ok(!snap.tabs.some((t) => t.path === "/c.yml"));
    assert.equal(snap.activePath, "/b.yml");
  });
});

describe("editor.zustand — closeToRight", () => {
  it("closes tabs to the right of the specified path", async () => {
    const editor = makeEditor({ "/a.yml": "", "/b.yml": "", "/c.yml": "" });
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    await editor.openFile("/c.yml");
    editor.closeToRight("/a.yml");
    const snap = editor.getSnapshot();
    assert.equal(snap.tabs.length, 1);
    assert.equal(snap.tabs[0]!.path, "/a.yml");
  });

  it("is a no-op for an unknown path", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.closeToRight("/unknown.yml");
    assert.equal(editor.getSnapshot().tabs.length, 1);
  });
});

describe("editor.zustand — closeAll", () => {
  it("removes all unpinned tabs", async () => {
    const editor = makeEditor({ "/a.yml": "", "/b.yml": "" });
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    editor.togglePinned("/a.yml");
    editor.closeAll();
    const snap = editor.getSnapshot();
    assert.equal(snap.tabs.length, 1);
    assert.equal(snap.tabs[0]!.path, "/a.yml");
  });

  it("sets activePath to null when all tabs are unpinned", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.closeAll();
    assert.equal(editor.getSnapshot().activePath, null);
  });
});

describe("editor.zustand — updateContent", () => {
  it("marks tab dirty when content differs from savedContent", async () => {
    const editor = makeEditor({ "/a.yml": "original" });
    await editor.openFile("/a.yml");
    editor.updateContent("/a.yml", "changed");
    const tab = editor.getSnapshot().tabs[0]!;
    assert.equal(tab.content, "changed");
    assert.equal(tab.dirty, true);
  });

  it("marks tab clean when content matches savedContent", async () => {
    const editor = makeEditor({ "/a.yml": "original" });
    await editor.openFile("/a.yml");
    editor.updateContent("/a.yml", "changed");
    editor.updateContent("/a.yml", "original");
    assert.equal(editor.getSnapshot().tabs[0]!.dirty, false);
  });

  it("is a no-op for an unknown path", async () => {
    const editor = makeEditor({ "/a.yml": "x" });
    await editor.openFile("/a.yml");
    editor.updateContent("/unknown.yml", "y");
    assert.equal(editor.getSnapshot().tabs[0]!.content, "x");
  });
});

describe("editor.zustand — saveActive", () => {
  it("clears dirty flag and updates savedContent", async () => {
    const editor = makeEditor({ "/a.yml": "original" }, false);
    await editor.openFile("/a.yml");
    editor.updateContent("/a.yml", "changed");
    await editor.saveActive();
    const tab = editor.getSnapshot().tabs[0]!;
    assert.equal(tab.dirty, false);
    assert.equal(tab.savedContent, "changed");
    assert.equal(tab.content, "changed");
  });

  it("strips trailing whitespace when setting is enabled", async () => {
    const ws = makeWorkspace({ "/a.yml": "line  " });
    let written = "";
    ws.writeScript = async (_p, c) => {
      written = c;
    };
    const editor = createZustandEditor({ workspace: ws, getSettings: () => makeSettings(true) });
    await editor.openFile("/a.yml");
    editor.updateContent("/a.yml", "line  ");
    await editor.saveActive();
    assert.equal(written, "line");
  });

  it("does not strip whitespace when setting is disabled", async () => {
    const ws = makeWorkspace({ "/a.yml": "line  " });
    let written = "";
    ws.writeScript = async (_p, c) => {
      written = c;
    };
    const editor = createZustandEditor({ workspace: ws, getSettings: () => makeSettings(false) });
    await editor.openFile("/a.yml");
    editor.updateContent("/a.yml", "line  ");
    await editor.saveActive();
    assert.equal(written, "line  ");
  });

  it("is a no-op when there is no active tab", async () => {
    const editor = makeEditor();
    await assert.doesNotReject(() => editor.saveActive());
  });
});

describe("editor.zustand — saveAll", () => {
  it("saves all dirty tabs and clears their dirty flags", async () => {
    const editor = makeEditor({ "/a.yml": "a", "/b.yml": "b" }, false);
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    editor.updateContent("/a.yml", "a2");
    editor.updateContent("/b.yml", "b2");
    await editor.saveAll();
    const snap = editor.getSnapshot();
    assert.ok(snap.tabs.every((t) => !t.dirty));
    assert.equal(snap.tabs.find((t) => t.path === "/a.yml")!.savedContent, "a2");
    assert.equal(snap.tabs.find((t) => t.path === "/b.yml")!.savedContent, "b2");
  });
});

describe("editor.zustand — reopenLastClosed", () => {
  it("reopens the last closed file", async () => {
    const editor = makeEditor({ "/a.yml": "hello" });
    await editor.openFile("/a.yml");
    editor.closeFile("/a.yml");
    assert.equal(editor.getSnapshot().tabs.length, 0);
    await editor.reopenLastClosed();
    assert.equal(editor.getSnapshot().tabs.length, 1);
    assert.equal(editor.getSnapshot().activePath, "/a.yml");
    assert.ok(!editor.getSnapshot().closedStack.includes("/a.yml"));
  });

  it("is a no-op when closedStack is empty", async () => {
    const editor = makeEditor();
    await assert.doesNotReject(() => editor.reopenLastClosed());
  });
});

describe("editor.zustand — renameTab", () => {
  it("updates path and activePath when renaming the active tab", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.renameTab("/a.yml", "/renamed.yml");
    const snap = editor.getSnapshot();
    assert.ok(snap.tabs.some((t) => t.path === "/renamed.yml"));
    assert.equal(snap.activePath, "/renamed.yml");
  });
});

describe("editor.zustand — nextTab / previousTab", () => {
  let editor: EditorApi;

  beforeEach(async () => {
    editor = makeEditor({ "/a.yml": "", "/b.yml": "", "/c.yml": "" });
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    await editor.openFile("/c.yml");
    editor.activate("/b.yml");
  });

  it("nextTab moves to the next tab", () => {
    editor.nextTab();
    assert.equal(editor.getSnapshot().activePath, "/c.yml");
  });

  it("nextTab wraps around from last to first", () => {
    editor.activate("/c.yml");
    editor.nextTab();
    assert.equal(editor.getSnapshot().activePath, "/a.yml");
  });

  it("previousTab moves to the previous tab", () => {
    editor.previousTab();
    assert.equal(editor.getSnapshot().activePath, "/a.yml");
  });

  it("previousTab wraps around from first to last", () => {
    editor.activate("/a.yml");
    editor.previousTab();
    assert.equal(editor.getSnapshot().activePath, "/c.yml");
  });

  it("nextTab is a no-op with only one tab", async () => {
    const single = makeEditor({ "/x.yml": "" });
    await single.openFile("/x.yml");
    single.nextTab();
    assert.equal(single.getSnapshot().activePath, "/x.yml");
  });
});

describe("editor.zustand — togglePinned", () => {
  it("pins and unpins a tab", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.togglePinned("/a.yml");
    assert.equal(editor.getSnapshot().tabs[0]!.pinned, true);
    editor.togglePinned("/a.yml");
    assert.equal(editor.getSnapshot().tabs[0]!.pinned, false);
  });
});

describe("editor.zustand — reloadFromDisk", () => {
  it("replaces content/savedContent and clears dirty", async () => {
    const editor = makeEditor({ "/a.yml": "old" });
    await editor.openFile("/a.yml");
    editor.updateContent("/a.yml", "modified");
    editor.reloadFromDisk("/a.yml", "fresh from disk");
    const tab = editor.getSnapshot().tabs[0]!;
    assert.equal(tab.content, "fresh from disk");
    assert.equal(tab.savedContent, "fresh from disk");
    assert.equal(tab.dirty, false);
  });
});

describe("editor.zustand — splitRight", () => {
  it("creates a second group containing the active tab", async () => {
    const editor = makeEditor({ "/a.yml": "x" });
    await editor.openFile("/a.yml");
    editor.splitRight();
    const snap = editor.getSnapshot();
    assert.equal(snap.groups.length, 2);
    assert.equal(snap.activePath, "/a.yml");
  });

  it("does not split when already at two groups", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.splitRight();
    editor.splitRight();
    assert.equal(editor.getSnapshot().groups.length, 2);
  });

  it("is a no-op when there is no active tab", () => {
    const editor = makeEditor();
    editor.splitRight();
    assert.equal(editor.getSnapshot().groups.length, 1);
  });
});

describe("editor.zustand — closeGroup / focusGroup", () => {
  it("closeGroup removes the group and falls back to primary", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.splitRight();
    const snap1 = editor.getSnapshot();
    const secondId = snap1.activeGroupId;
    editor.closeGroup(secondId);
    const snap2 = editor.getSnapshot();
    assert.equal(snap2.groups.length, 1);
    assert.notEqual(snap2.activeGroupId, secondId);
  });

  it("closeGroup is a no-op when only one group remains", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    const { activeGroupId } = editor.getSnapshot();
    editor.closeGroup(activeGroupId);
    assert.equal(editor.getSnapshot().groups.length, 1);
  });

  it("focusGroup switches the active group", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.splitRight();
    const snap = editor.getSnapshot();
    const firstGroupId = snap.groups[0]!.id;
    editor.focusGroup(firstGroupId);
    assert.equal(editor.getSnapshot().activeGroupId, firstGroupId);
  });

  it("focusGroup is a no-op for an unknown group id", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    const before = editor.getSnapshot().activeGroupId;
    editor.focusGroup("does-not-exist");
    assert.equal(editor.getSnapshot().activeGroupId, before);
  });
});

describe("editor.zustand — subscribe", () => {
  it("notifies listener on state change", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    let calls = 0;
    const unsub = editor.subscribe(() => { calls++; });
    await editor.openFile("/a.yml");
    unsub();
    assert.ok(calls >= 1);
  });

  it("does not notify after unsubscribe", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    let calls = 0;
    const unsub = editor.subscribe(() => { calls++; });
    unsub();
    await editor.openFile("/a.yml");
    assert.equal(calls, 0);
  });
});

describe("editor.zustand — snapshot mirror fields", () => {
  it("tabs and activePath mirror the active group", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.splitRight();
    const snap = editor.getSnapshot();
    assert.deepEqual(
      snap.tabs,
      snap.groups.find((g) => g.id === snap.activeGroupId)!.tabs,
    );
    assert.equal(
      snap.activePath,
      snap.groups.find((g) => g.id === snap.activeGroupId)!.activePath,
    );
  });
});

describe("editor.zustand — branch coverage supplements", () => {
  it("closeOthers: maps over non-active groups without changing them", async () => {
    // Exercises the g.id !== activeGroup.id branch in closeOthers groups.map
    const editor = makeEditor({ "/a.yml": "", "/b.yml": "" });
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    editor.splitRight();
    // Now there are two groups; closeOthers on the active (second) group
    editor.closeOthers("/b.yml");
    const snap = editor.getSnapshot();
    // Active group should only have /b.yml
    const activeGroup = snap.groups.find((g) => g.id === snap.activeGroupId)!;
    assert.ok(activeGroup.tabs.every((t) => t.path === "/b.yml"));
  });

  it("closeToRight: activePath stays when it is not in the closed set", async () => {
    const editor = makeEditor({ "/a.yml": "", "/b.yml": "", "/c.yml": "" });
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    await editor.openFile("/c.yml");
    // activate /a.yml — it will NOT be in the closed set (it's to the left of /b.yml)
    editor.activate("/a.yml");
    editor.closeToRight("/b.yml");
    // /c.yml is closed; active was /a.yml which is not in closed → stays /a.yml
    assert.equal(editor.getSnapshot().activePath, "/a.yml");
  });

  it("saveAll: does not mutate tabs that are not dirty", async () => {
    const editor = makeEditor({ "/a.yml": "a", "/b.yml": "b" }, false);
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    // Only /a.yml is dirty
    editor.updateContent("/a.yml", "a2");
    await editor.saveAll();
    // /b.yml should be unchanged (content still "b")
    const tabB = editor.getSnapshot().tabs.find((t) => t.path === "/b.yml")!;
    assert.equal(tabB.content, "b");
    assert.equal(tabB.dirty, false);
  });

  it("reloadFromDisk: leaves non-matching tabs untouched", async () => {
    const editor = makeEditor({ "/a.yml": "a", "/b.yml": "b" });
    await editor.openFile("/a.yml");
    await editor.openFile("/b.yml");
    editor.reloadFromDisk("/a.yml", "reloaded");
    const tabB = editor.getSnapshot().tabs.find((t) => t.path === "/b.yml")!;
    assert.equal(tabB.content, "b");
  });

  it("closeGroup: keeps activeGroupId when closing a non-active group", async () => {
    const editor = makeEditor({ "/a.yml": "" });
    await editor.openFile("/a.yml");
    editor.splitRight();
    const snap1 = editor.getSnapshot();
    const activeId = snap1.activeGroupId;
    const otherId = snap1.groups.find((g) => g.id !== activeId)!.id;
    editor.closeGroup(otherId);
    assert.equal(editor.getSnapshot().activeGroupId, activeId);
    assert.equal(editor.getSnapshot().groups.length, 1);
  });
});

describe("editor.zustand — closedStack cap", () => {
  it("caps closedStack at 20 entries", async () => {
    const files: Record<string, string> = {};
    for (let i = 0; i < 25; i++) files[`/f${i}.yml`] = "";
    const editor = makeEditor(files);
    for (let i = 0; i < 25; i++) await editor.openFile(`/f${i}.yml`);
    for (let i = 0; i < 25; i++) editor.closeFile(`/f${i}.yml`);
    assert.ok(editor.getSnapshot().closedStack.length <= 20);
  });
});
