import type { ProjectRegistry } from "./registry.port.js";
import type { RecentProject } from "./types.js";

/**
 * In-memory registry for UI and state tests. Ids are a simple
 * djb2 hash of the path so the mock stays stable across runs and
 * test fixtures can precompute them.
 */
export class MockProjectRegistry implements ProjectRegistry {
  private readonly entries = new Map<string, RecentProject>();
  private clock: () => number;

  constructor(options?: { now?: () => number }) {
    this.clock = options?.now ?? Date.now;
  }

  async list(): Promise<readonly RecentProject[]> {
    return Array.from(this.entries.values());
  }

  async touch(path: string): Promise<RecentProject> {
    const id = hashId(path);
    const now = this.clock();
    const existing = this.entries.get(id);
    const next: RecentProject = existing
      ? { ...existing, lastOpenedAt: now }
      : {
          id,
          path,
          displayName: basename(path),
          pinned: false,
          lastOpenedAt: now,
          addedAt: now,
        };
    this.entries.set(id, next);
    return next;
  }

  async setPinned(id: string, pinned: boolean): Promise<void> {
    const current = this.entries.get(id);
    if (!current) return;
    this.entries.set(id, { ...current, pinned });
  }

  async remove(id: string): Promise<void> {
    this.entries.delete(id);
  }
}

function hashId(path: string): string {
  let h = 5381;
  for (let i = 0; i < path.length; i++) {
    h = ((h << 5) + h + path.charCodeAt(i)) >>> 0;
  }
  return h.toString(36);
}

function basename(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  const idx = trimmed.lastIndexOf("/");
  return idx === -1 ? trimmed : trimmed.slice(idx + 1);
}
