import type {
  RawTheme,
  ThemeStore,
  ThemeWatchCallback,
} from "./theme-store.port.js";
import type { Theme } from "./types.js";

/**
 * In-memory `ThemeStore` used by tests. Seed with a list of
 * built-ins and (optionally) a list of user themes; the mock
 * round-trips `saveUser` into the in-memory user list so store-
 * consumer tests can exercise full CRUD.
 */
export interface MockThemeStoreFixture {
  readonly builtIns?: readonly unknown[];
  readonly users?: readonly unknown[];
}

export class MockThemeStore implements ThemeStore {
  private readonly builtIns: unknown[];
  private readonly users: Map<string, unknown>;
  private readonly watchers = new Set<ThemeWatchCallback>();

  constructor(fixture: MockThemeStoreFixture = {}) {
    this.builtIns = [...(fixture.builtIns ?? [])];
    this.users = new Map();
    for (const t of fixture.users ?? []) {
      const id = (t as { id?: string }).id;
      if (typeof id === "string") this.users.set(id, t);
    }
  }

  async listBuiltIns(): Promise<readonly RawTheme[]> {
    return this.builtIns.map((json) => ({
      source: "built-in" as const,
      path: null,
      json,
    }));
  }

  async listUser(): Promise<readonly RawTheme[]> {
    return Array.from(this.users.entries()).map(([id, json]) => ({
      source: "user" as const,
      path: `mock://themes/${id}.json`,
      json,
    }));
  }

  async listWorkspace(_path: string): Promise<readonly RawTheme[]> {
    return [];
  }

  async loadById(id: string): Promise<RawTheme | null> {
    const user = this.users.get(id);
    if (user) {
      return { source: "user", path: `mock://themes/${id}.json`, json: user };
    }
    for (const json of this.builtIns) {
      if ((json as { id?: string }).id === id) {
        return { source: "built-in", path: null, json };
      }
    }
    return null;
  }

  async saveUser(theme: Theme): Promise<void> {
    this.users.set(theme.id, theme);
  }

  async deleteUser(id: string): Promise<void> {
    this.users.delete(id);
  }

  async openThemesDir(): Promise<void> {
    /* no-op in mock */
  }

  watch(callback: ThemeWatchCallback): () => void {
    this.watchers.add(callback);
    return () => {
      this.watchers.delete(callback);
    };
  }

  /**
   * Test helper — fire a watcher event as if the filesystem
   * notified us.
   */
  emit(event: Parameters<ThemeWatchCallback>[0]): void {
    for (const cb of this.watchers) cb(event);
  }
}
