import type { FileTree } from "./workspace.types.js";
import type { WorkspaceStore } from "./workspace.port.js";

interface MockWorkspaceFixture {
  readonly trees?: Readonly<Record<string, FileTree>>;
  readonly files?: Readonly<Record<string, string>>;
  readonly pickFolderResult?: string | null;
}

/**
 * In-memory `WorkspaceStore`. All paths are just keys — no real
 * path resolution. UI tests can seed trees and file contents.
 */
export class MockWorkspaceStore implements WorkspaceStore {
  private readonly trees = new Map<string, FileTree>();
  private readonly files = new Map<string, string>();
  private readonly pickFolderResult: string | null;

  constructor(fixture: MockWorkspaceFixture = {}) {
    if (fixture.trees) {
      for (const [k, v] of Object.entries(fixture.trees)) this.trees.set(k, v);
    }
    if (fixture.files) {
      for (const [k, v] of Object.entries(fixture.files)) this.files.set(k, v);
    }
    this.pickFolderResult = fixture.pickFolderResult ?? null;
  }

  async openFolder(path: string): Promise<FileTree> {
    const tree = this.trees.get(path);
    if (!tree) {
      throw new Error(`MockWorkspaceStore: no tree configured for "${path}"`);
    }
    return tree;
  }

  async readScript(path: string): Promise<string> {
    const contents = this.files.get(path);
    if (contents === undefined) {
      throw new Error(`MockWorkspaceStore: file "${path}" not seeded`);
    }
    return contents;
  }

  async writeScript(path: string, content: string): Promise<void> {
    this.files.set(path, content);
  }

  async createScript(
    parentPath: string,
    fileName: string,
    content: string,
  ): Promise<string> {
    const path = `${parentPath.replace(/\/$/, "")}/${fileName}`;
    this.files.set(path, content);
    return path;
  }

  async createFolder(
    parentPath: string,
    folderName: string,
  ): Promise<string> {
    return `${parentPath.replace(/\/$/, "")}/${folderName}`;
  }

  async deleteScript(path: string): Promise<void> {
    this.files.delete(path);
  }

  async renameScript(path: string, newName: string): Promise<string> {
    const slash = path.lastIndexOf("/");
    const parent = slash >= 0 ? path.slice(0, slash) : "";
    const newPath = parent.length > 0 ? `${parent}/${newName}` : newName;
    const contents = this.files.get(path);
    if (contents !== undefined) {
      this.files.delete(path);
      this.files.set(newPath, contents);
    }
    return newPath;
  }

  async pickFolder(): Promise<string | null> {
    return this.pickFolderResult;
  }
}
