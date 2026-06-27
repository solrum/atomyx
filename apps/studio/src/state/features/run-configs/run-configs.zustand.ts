import { createStore } from "zustand/vanilla";
import {
  makeConfigId,
  RUN_CONFIGS_SCHEMA_VERSION,
  type RunConfig,
  type RunConfigsFile,
} from "../../../domain/features/run-configs/index.js";
import type { ProjectConfigApi } from "../project-config/index.js";
import type {
  RunConfigsApi,
  RunConfigsSnapshot,
} from "./run-configs.contract.js";

const RUN_CONFIGS_FILE = "run-configs.json";

export interface RunConfigsDeps {
  readonly projectConfig: ProjectConfigApi;
  readonly getLastActiveRunConfig: () => string | null | undefined;
  readonly setLastActiveRunConfig: (id: string | null) => void;
}

export function createZustandRunConfigs(deps: RunConfigsDeps): RunConfigsApi {
  const { projectConfig, getLastActiveRunConfig, setLastActiveRunConfig } = deps;
  const store = createStore<RunConfigsSnapshot>(() => ({
    workspacePath: null,
    configs: [],
    activeId: null,
  }));

  async function persist(
    workspacePath: string | null,
    configs: readonly RunConfig[],
  ): Promise<void> {
    if (!workspacePath) return;
    const file: RunConfigsFile = {
      schemaVersion: RUN_CONFIGS_SCHEMA_VERSION,
      configs,
    };
    await projectConfig.writeJson(RUN_CONFIGS_FILE, file);
  }

  const api: RunConfigsApi = {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    async hydrate(workspacePath) {
      const file = await projectConfig.readJson<RunConfigsFile>(
        RUN_CONFIGS_FILE,
      );
      const configs = file?.configs ?? [];
      const persistedActive = getLastActiveRunConfig() ?? configs[0]?.id ?? null;
      store.setState({
        workspacePath,
        configs,
        activeId:
          persistedActive && configs.some((c) => c.id === persistedActive)
            ? persistedActive
            : null,
      });
    },

    setActive(id) {
      store.setState({ activeId: id });
      setLastActiveRunConfig(id);
    },

    async save(patch) {
      const current = store.getState();
      const existing = patch.id
        ? current.configs.find((c) => c.id === patch.id)
        : undefined;
      const config: RunConfig = existing
        ? {
            ...existing,
            name: patch.name ?? existing.name,
            deviceId: patch.deviceId ?? existing.deviceId,
            appId: patch.appId ?? existing.appId,
            scriptPath: patch.scriptPath ?? existing.scriptPath,
            env: patch.env ?? existing.env,
          }
        : {
            id: patch.id ?? makeConfigId(patch.name),
            name: patch.name,
            deviceId: patch.deviceId ?? null,
            appId: patch.appId ?? null,
            scriptPath: patch.scriptPath ?? null,
            env: patch.env ?? {},
          };
      const next = existing
        ? current.configs.map((c) => (c.id === config.id ? config : c))
        : [...current.configs, config];
      await persist(current.workspacePath, next);
      store.setState({ configs: next });
      if (!store.getState().activeId) {
        api.setActive(config.id);
      }
      return config;
    },

    async remove(id) {
      const current = store.getState();
      const next = current.configs.filter((c) => c.id !== id);
      await persist(current.workspacePath, next);
      const activeId =
        current.activeId === id ? (next[0]?.id ?? null) : current.activeId;
      store.setState({ configs: next, activeId });
    },

    async duplicate(id) {
      const current = store.getState();
      const source = current.configs.find((c) => c.id === id);
      if (!source) return null;
      const copy: RunConfig = {
        ...source,
        id: makeConfigId(source.name),
        name: `${source.name} (copy)`,
      };
      const next = [...current.configs, copy];
      await persist(current.workspacePath, next);
      store.setState({ configs: next });
      return copy;
    },
  };

  return api;
}
