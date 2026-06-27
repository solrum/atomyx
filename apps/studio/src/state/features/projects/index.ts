import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  ProjectsApi,
  ProjectsSnapshot,
  RecentProject,
} from "./projects.contract.js";
import {
  createZustandProjects,
  type ProjectsDeps,
} from "./projects.zustand.js";

export type { ProjectsApi, ProjectsSnapshot, RecentProject };

export const PROJECTS_KEY = "projects";

export function createProjects(deps: ProjectsDeps): ProjectsApi {
  return createZustandProjects(deps);
}

export function useProjects(): ProjectsSnapshot &
  Pick<ProjectsApi, "reload" | "touch" | "setPinned" | "remove"> {
  const api = getFeature<ProjectsApi>(PROJECTS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    reload: api.reload,
    touch: api.touch,
    setPinned: api.setPinned,
    remove: api.remove,
  };
}
