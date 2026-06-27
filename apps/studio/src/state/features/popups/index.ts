import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  PopupId,
  PopupsApi,
  PopupsSnapshot,
} from "./popups.contract.js";
import { createZustandPopups } from "./popups.zustand.js";

export type { PopupId, PopupsApi, PopupsSnapshot };

export const POPUPS_KEY = "popups";

export function createPopups(): PopupsApi {
  return createZustandPopups();
}

export function usePopups(): PopupsSnapshot &
  Pick<PopupsApi, "open" | "close" | "closeAll" | "isOpen"> {
  const api = getFeature<PopupsApi>(POPUPS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    open: api.open,
    close: api.close,
    closeAll: api.closeAll,
    isOpen: api.isOpen,
  };
}
