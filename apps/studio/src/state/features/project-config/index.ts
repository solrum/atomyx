import type {
  ProjectConfigApi,
  ProjectConfigStore,
} from "./project-config.contract.js";
import {
  createProjectConfigFeature,
  type ProjectConfigDeps,
} from "./project-config.impl.js";

export type { ProjectConfigApi, ProjectConfigStore };

export const PROJECT_CONFIG_KEY = "project-config";

export function createProjectConfig(
  deps: ProjectConfigDeps,
): ProjectConfigApi {
  return createProjectConfigFeature(deps);
}

