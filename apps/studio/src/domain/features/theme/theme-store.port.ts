import type { Theme } from "./theme.types.js";

/**
 * Raw theme entry — what a ThemeStore returns before zod
 * validation. Consumers run it through `parseTheme` to get a
 * typed `Theme`.
 */
export interface RawTheme {
  readonly source: "built-in" | "user" | "workspace";
  readonly path: string | null;
  readonly json: unknown;
}

export type ThemeChangeEvent =
  | { readonly type: "added"; readonly theme: Theme }
  | { readonly type: "updated"; readonly theme: Theme }
  | { readonly type: "removed"; readonly themeId: string };

export type ThemeWatchCallback = (event: ThemeChangeEvent) => void;

/**
 * Contract for discovering and editing Atomyx themes. Built-ins
 * are read-only (bundled in the app); user themes live on disk.
 * Live-reload is provided by `watch` when the backend supports
 * filesystem events.
 */
export interface ThemeStore {
  listBuiltIns(): Promise<readonly RawTheme[]>;
  listUser(): Promise<readonly RawTheme[]>;
  /**
   * Project-local themes discovered under
   * `<workspacePath>/.atomyx/themes/`. Returns empty when the
   * folder is absent — not an error.
   */
  listWorkspace(workspacePath: string): Promise<readonly RawTheme[]>;
  loadById(id: string): Promise<RawTheme | null>;
  saveUser(theme: Theme): Promise<void>;
  deleteUser(id: string): Promise<void>;
  openThemesDir(): Promise<void>;
  /**
   * Subscribe to live updates from the user themes folder.
   * Returns a disposer. Adapters without fs watching may
   * implement this as a no-op; callers should also expose a
   * "Reload themes" action.
   */
  watch(callback: ThemeWatchCallback): () => void;
}
