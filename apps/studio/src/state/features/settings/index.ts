import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  SettingsApi,
  SettingsSnapshot,
  StudioSettings,
} from "./settings.contract.js";
import {
  createZustandSettings,
  type SettingsDeps,
} from "./settings.zustand.js";

export type { SettingsApi, SettingsSnapshot, StudioSettings };

export const SETTINGS_KEY = "settings";

export function createSettings(deps: SettingsDeps): SettingsApi {
  return createZustandSettings(deps);
}

export function useSettings(): SettingsSnapshot &
  Pick<SettingsApi, "load" | "update"> {
  const api = getFeature<SettingsApi>(SETTINGS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return { ...snap, load: api.load, update: api.update };
}
