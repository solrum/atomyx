/**
 * Read / write arbitrary files under a workspace's `.atomyx/`
 * configuration directory.
 *
 * Scope discipline: `relPath` is a relative path interpreted from
 * `<workspace>/.atomyx/`. Backends MUST reject absolute paths and
 * any `..` segment so a caller can't escape the config folder —
 * the renderer passes user-facing strings into `relPath` for some
 * call-sites (e.g. a future settings editor) so path validation
 * is load-bearing, not a nicety.
 *
 * Non-existent files are reported as `null` from the read calls;
 * callers substitute defaults rather than distinguishing "missing"
 * from "empty". Writes create intermediate directories as needed.
 *
 * This port handles JSON and plain text — YAML is written as text
 * and parsed in the renderer because each consumer owns its zod
 * schema and doesn't need the backend to understand YAML.
 */
export interface ProjectConfigStore {
  readJson<T>(workspacePath: string, relPath: string): Promise<T | null>;
  writeJson<T>(
    workspacePath: string,
    relPath: string,
    value: T,
  ): Promise<void>;
  readText(
    workspacePath: string,
    relPath: string,
  ): Promise<string | null>;
  writeText(
    workspacePath: string,
    relPath: string,
    content: string,
  ): Promise<void>;
  /**
   * Parse every `*.json` file under `<workspace>/.atomyx/<relPath>/`
   * and return the decoded JSON bodies. Missing directory yields
   * an empty array; unreadable / invalid JSON files are silently
   * skipped so one broken user file doesn't collapse the whole
   * listing. Used by features that host a folder full of
   * homogeneous config entries (themes, and future shared
   * fixtures) instead of a single file.
   */
  listJsonDirectory(
    workspacePath: string,
    relPath: string,
  ): Promise<readonly unknown[]>;
}
