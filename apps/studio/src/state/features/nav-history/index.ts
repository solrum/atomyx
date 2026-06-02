import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  NavHistoryApi,
  NavHistorySnapshot,
  NavLocation,
} from "./nav-history.contract.js";
import { createZustandNavHistory } from "./nav-history.zustand.js";

export type { NavHistoryApi, NavHistorySnapshot, NavLocation };

export const NAV_HISTORY_KEY = "nav-history";

export function createNavHistory(): NavHistoryApi {
  return createZustandNavHistory();
}

export function useNavHistory(): NavHistorySnapshot &
  Pick<
    NavHistoryApi,
    "record" | "back" | "forward" | "clear" | "beginNavigation" | "endNavigation"
  > {
  const api = getFeature<NavHistoryApi>(NAV_HISTORY_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    record: api.record,
    back: api.back,
    forward: api.forward,
    clear: api.clear,
    beginNavigation: api.beginNavigation,
    endNavigation: api.endNavigation,
  };
}
