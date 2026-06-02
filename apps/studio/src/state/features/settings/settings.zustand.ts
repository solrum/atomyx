import { createStore } from "zustand/vanilla";
import {
  DEFAULT_SETTINGS,
  type StudioSettings,
  type SettingsStore as SettingsPort,
} from "../../../domain/features/settings/index.js";
import type { SettingsApi, SettingsSnapshot } from "./settings.contract.js";

export interface SettingsDeps {
  readonly store: SettingsPort;
}

export function createZustandSettings(deps: SettingsDeps): SettingsApi {
  const { store: port } = deps;
  const store = createStore<SettingsSnapshot>(() => ({
    settings: DEFAULT_SETTINGS,
    loaded: false,
  }));

  return {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    async load() {
      const loaded = await port.load();
      store.setState({ settings: loaded, loaded: true });
    },

    async update(patch: Partial<StudioSettings>) {
      const merged = { ...store.getState().settings, ...patch };
      await port.save(merged);
      store.setState({ settings: merged });
    },
  };
}
