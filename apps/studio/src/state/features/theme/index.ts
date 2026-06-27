import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  EffectiveAttributes,
  Theme,
  ThemeApi,
  ThemeListEntry,
  ThemeOverrides,
  ThemeSnapshot,
} from "./theme.contract.js";
import { createZustandTheme, type ThemeDeps } from "./theme.zustand.js";

export type {
  EffectiveAttributes,
  Theme,
  ThemeApi,
  ThemeListEntry,
  ThemeOverrides,
  ThemeSnapshot,
};

export const THEME_KEY = "theme";

export function createTheme(deps: ThemeDeps): ThemeApi {
  return createZustandTheme(deps);
}

export function useThemes(): ThemeSnapshot &
  Pick<
    ThemeApi,
    "reload" | "setActiveId" | "setOverride" | "clearOverrides" | "openThemesDir"
  > {
  const api = getFeature<ThemeApi>(THEME_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    reload: api.reload,
    setActiveId: api.setActiveId,
    setOverride: api.setOverride,
    clearOverrides: api.clearOverrides,
    openThemesDir: api.openThemesDir,
  };
}
