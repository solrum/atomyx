import { createStore } from "zustand/vanilla";
import {
  type RecentProject,
  sortRecentProjects,
} from "../../../domain/features/projects/index.js";
import type { ProjectRegistry } from "../../../domain/features/projects/index.js";
import type { ProjectsApi, ProjectsSnapshot } from "./projects.contract.js";

export interface ProjectsDeps {
  readonly registry: ProjectRegistry;
}

export function createZustandProjects(deps: ProjectsDeps): ProjectsApi {
  const store = createStore<ProjectsSnapshot>(() => ({ items: [] }));
  const { registry } = deps;

  async function refresh(): Promise<readonly RecentProject[]> {
    const current = await registry.list();
    const items = sortRecentProjects(current);
    store.setState({ items });
    return items;
  }

  return {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    reload: async () => {
      await refresh();
    },

    async touch(path) {
      const updated = await registry.touch(path);
      await refresh();
      return updated;
    },

    async setPinned(id, pinned) {
      await registry.setPinned(id, pinned);
      await refresh();
    },

    async remove(id) {
      await registry.remove(id);
      await refresh();
    },
  };
}
