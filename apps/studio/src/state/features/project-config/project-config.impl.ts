import type {
  ProjectConfigApi,
  ProjectConfigStore,
} from "./project-config.contract.js";

export interface ProjectConfigDeps {
  readonly port: ProjectConfigStore;
  readonly getWorkspacePath: () => string | null;
}

/**
 * Every mutating call asserts there is an open workspace — there
 * is no well-defined "`.atomyx/`" without one, and silently
 * swallowing writes would hide bugs (e.g. a consumer calling
 * `writeJson` before workspace-open). Reads return null for the
 * same reason a missing file would, since the UX is equivalent.
 */
export function createProjectConfigFeature(
  deps: ProjectConfigDeps,
): ProjectConfigApi {
  const { port, getWorkspacePath } = deps;

  const requireWorkspace = (op: string): string => {
    const path = getWorkspacePath();
    if (!path) {
      throw new Error(
        `project-config ${op}: no workspace is open`,
      );
    }
    return path;
  };

  return {
    hasWorkspace: () => getWorkspacePath() !== null,
    readJson: async (relPath) => {
      const workspace = getWorkspacePath();
      if (!workspace) return null;
      return port.readJson(workspace, relPath);
    },
    writeJson: async (relPath, value) => {
      const workspace = requireWorkspace("writeJson");
      return port.writeJson(workspace, relPath, value);
    },
    readText: async (relPath) => {
      const workspace = getWorkspacePath();
      if (!workspace) return null;
      return port.readText(workspace, relPath);
    },
    writeText: async (relPath, content) => {
      const workspace = requireWorkspace("writeText");
      return port.writeText(workspace, relPath, content);
    },
  };
}
