import type { ProjectConfigStore } from "../../../domain/features/project-config/index.js";

export type { ProjectConfigStore };

/**
 * Thin state-layer wrapper over `ProjectConfigStore` that resolves
 * the workspace path on every call. Consumers read / write files
 * under the current workspace's `.atomyx/` without carrying the
 * workspace path at every call site — the feature binds to
 * `getWorkspacePath()` at construction time.
 *
 * `relPath` is interpreted from `<workspace>/.atomyx/`. Calls
 * reject when no workspace is open.
 */
export interface ProjectConfigApi {
  readonly hasWorkspace: () => boolean;
  readJson<T>(relPath: string): Promise<T | null>;
  writeJson<T>(relPath: string, value: T): Promise<void>;
  readText(relPath: string): Promise<string | null>;
  writeText(relPath: string, content: string): Promise<void>;
}
