import type { FileEntry, FileTree } from "./workspace.types.js";

export interface FlatFileNode {
  readonly path: string;
  readonly name: string;
  readonly parent: string;
}

/**
 * Collect every leaf file path in the tree, in DFS order. Used by
 * cross-feature search surfaces ("Find Everywhere") that need a
 * flat list of files independent of the directory hierarchy.
 */
export function collectFilePaths(tree: FileTree | null): readonly string[] {
  if (!tree) return [];
  const out: string[] = [];
  walkFilesInto(tree.entries, out);
  return out;
}

function walkFilesInto(entries: readonly FileEntry[], out: string[]): void {
  for (const e of entries) {
    if (e.type === "file") {
      out.push(e.path);
    } else if (e.children) {
      walkFilesInto(e.children, out);
    }
  }
}

/**
 * Collect every directory path in the tree, used by Collapse-All
 * to build the initial collapsed-set covering the whole project.
 */
export function collectAllDirs(tree: FileTree | null): ReadonlySet<string> {
  const out = new Set<string>();
  if (!tree) return out;
  collectAllDirsInto(tree.entries, out);
  return out;
}

function collectAllDirsInto(
  entries: readonly FileEntry[],
  out: Set<string>,
): void {
  for (const e of entries) {
    if (e.type === "directory") {
      out.add(e.path);
      if (e.children) collectAllDirsInto(e.children, out);
    }
  }
}

/**
 * Flatten the tree's leaf files into `{path, name, parent}` rows
 * suitable for a "Go to file" picker. Directories are skipped;
 * each file's `parent` is the absolute path of its containing
 * directory (or the tree root for top-level files).
 */
export function flattenFileTree(
  tree: FileTree | null,
): readonly FlatFileNode[] {
  if (!tree) return [];
  const out: FlatFileNode[] = [];
  flattenInto(tree.entries, tree.rootPath, out);
  return out;
}

function flattenInto(
  entries: readonly FileEntry[],
  parent: string,
  out: FlatFileNode[],
): void {
  for (const e of entries) {
    if (e.type === "directory") {
      flattenInto(e.children ?? [], e.path, out);
    } else {
      out.push({ path: e.path, name: e.name, parent });
    }
  }
}

/**
 * Return a pruned tree containing only entries whose name matches
 * `query` (case-insensitive substring) plus the ancestors needed
 * to reach them. An empty/whitespace query returns the input
 * unchanged. A directory is kept when its own name matches OR any
 * descendant survives the filter.
 */
export function filterFileTree(tree: FileTree, query: string): FileTree {
  const q = query.trim().toLowerCase();
  if (q.length === 0) return tree;
  return { ...tree, entries: filterEntries(tree.entries, q) };
}

function filterEntries(
  entries: readonly FileEntry[],
  needle: string,
): FileEntry[] {
  const out: FileEntry[] = [];
  for (const e of entries) {
    if (e.type === "directory") {
      const children = filterEntries(e.children ?? [], needle);
      if (children.length > 0 || e.name.toLowerCase().includes(needle)) {
        out.push({ ...e, children });
      }
    } else if (e.name.toLowerCase().includes(needle)) {
      out.push(e);
    }
  }
  return out;
}
