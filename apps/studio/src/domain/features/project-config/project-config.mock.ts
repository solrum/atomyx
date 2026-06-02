import type { ProjectConfigStore } from "./project-config.port.js";

/**
 * In-memory ProjectConfigStore. Files are keyed by
 * `<workspacePath>::<relPath>` so the same `relPath` in two
 * workspaces stays independent. JSON values round-trip via
 * `structuredClone` so tests can't smuggle a live reference
 * back out of the store.
 */
export class MockProjectConfigStore implements ProjectConfigStore {
  private readonly files = new Map<string, string>();

  private key(workspacePath: string, relPath: string): string {
    return `${workspacePath}::${relPath}`;
  }

  async readJson<T>(
    workspacePath: string,
    relPath: string,
  ): Promise<T | null> {
    const raw = this.files.get(this.key(workspacePath, relPath));
    if (raw === undefined) return null;
    if (raw.trim().length === 0) return null;
    return JSON.parse(raw) as T;
  }

  async writeJson<T>(
    workspacePath: string,
    relPath: string,
    value: T,
  ): Promise<void> {
    const cloned = structuredClone(value);
    this.files.set(
      this.key(workspacePath, relPath),
      JSON.stringify(cloned, null, 2),
    );
  }

  async readText(
    workspacePath: string,
    relPath: string,
  ): Promise<string | null> {
    return this.files.get(this.key(workspacePath, relPath)) ?? null;
  }

  async writeText(
    workspacePath: string,
    relPath: string,
    content: string,
  ): Promise<void> {
    this.files.set(this.key(workspacePath, relPath), content);
  }

  async listJsonDirectory(
    workspacePath: string,
    relPath: string,
  ): Promise<readonly unknown[]> {
    // Naive prefix scan — the real adapter reads a filesystem
    // directory, but for tests we just match on the key prefix.
    const prefix = this.key(workspacePath, relPath).replace(/\/+$/, "") + "/";
    const out: unknown[] = [];
    for (const [key, raw] of this.files.entries()) {
      if (!key.startsWith(prefix)) continue;
      const tail = key.slice(prefix.length);
      if (tail.includes("/")) continue;
      if (!tail.endsWith(".json")) continue;
      try {
        out.push(JSON.parse(raw));
      } catch {
        // Match the real adapter: silently skip broken entries.
      }
    }
    return out;
  }
}
