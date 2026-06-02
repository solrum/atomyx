import { test } from "node:test";
import { strict as assert } from "node:assert";
import type { FileEntry, FileTree } from "./types.js";
import {
  collectAllDirs,
  collectFilePaths,
  filterFileTree,
  flattenFileTree,
} from "./file-tree-walk.js";

function file(path: string, name: string = path.split("/").pop()!): FileEntry {
  return { type: "file", path, name };
}

function dir(
  path: string,
  children: readonly FileEntry[],
  name: string = path.split("/").pop()!,
): FileEntry {
  return { type: "directory", path, name, children };
}

const TREE: FileTree = {
  rootPath: "/proj",
  entries: [
    dir("/proj/src", [
      file("/proj/src/a.ts"),
      dir("/proj/src/sub", [file("/proj/src/sub/b.ts")]),
    ]),
    file("/proj/readme.md"),
  ],
};

test("collectFilePaths", async (t) => {
  await t.test("returns empty for null", () => {
    assert.deepEqual(collectFilePaths(null), []);
  });

  await t.test("collects files in DFS order, skipping dirs", () => {
    assert.deepEqual(collectFilePaths(TREE), [
      "/proj/src/a.ts",
      "/proj/src/sub/b.ts",
      "/proj/readme.md",
    ]);
  });
});

test("collectAllDirs", async (t) => {
  await t.test("returns empty set for null", () => {
    assert.equal(collectAllDirs(null).size, 0);
  });

  await t.test("collects every directory path", () => {
    assert.deepEqual(
      [...collectAllDirs(TREE)].sort(),
      ["/proj/src", "/proj/src/sub"],
    );
  });
});

test("flattenFileTree", async (t) => {
  await t.test("returns empty for null", () => {
    assert.deepEqual(flattenFileTree(null), []);
  });

  await t.test("flattens leaves with parent set to containing dir", () => {
    const flat = flattenFileTree(TREE);
    assert.deepEqual(flat, [
      { path: "/proj/src/a.ts", name: "a.ts", parent: "/proj/src" },
      { path: "/proj/src/sub/b.ts", name: "b.ts", parent: "/proj/src/sub" },
      { path: "/proj/readme.md", name: "readme.md", parent: "/proj" },
    ]);
  });
});

test("filterFileTree", async (t) => {
  await t.test("returns the original tree for an empty query", () => {
    assert.equal(filterFileTree(TREE, ""), TREE);
    assert.equal(filterFileTree(TREE, "  "), TREE);
  });

  await t.test("keeps ancestors of matching leaves", () => {
    const filtered = filterFileTree(TREE, "b.ts");
    assert.equal(filtered.entries.length, 1);
    assert.equal(filtered.entries[0]!.path, "/proj/src");
    const subDir = (filtered.entries[0] as FileEntry & { children: readonly FileEntry[] }).children[0]!;
    assert.equal(subDir.path, "/proj/src/sub");
  });

  await t.test("matches by directory name and keeps full subtree implicitly", () => {
    const filtered = filterFileTree(TREE, "sub");
    assert.equal(filtered.entries.length, 1);
    const sub = (filtered.entries[0] as FileEntry & { children: readonly FileEntry[] }).children;
    assert.equal(sub.length, 1);
    assert.equal(sub[0]!.path, "/proj/src/sub");
  });

  await t.test("drops branches with no match", () => {
    const filtered = filterFileTree(TREE, "readme");
    assert.equal(filtered.entries.length, 1);
    assert.equal(filtered.entries[0]!.path, "/proj/readme.md");
  });

  await t.test("is case-insensitive", () => {
    const filtered = filterFileTree(TREE, "README");
    assert.equal(filtered.entries.length, 1);
    assert.equal(filtered.entries[0]!.path, "/proj/readme.md");
  });
});
