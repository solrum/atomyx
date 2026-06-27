import { createStore } from "zustand/vanilla";
import type { PopupId, PopupsApi, PopupsSnapshot } from "./popups.contract.js";

export function createZustandPopups(): PopupsApi {
  const store = createStore<PopupsSnapshot>(() => ({
    openIds: new Set<PopupId>(),
  }));

  const withOpen = (mutator: (ids: Set<PopupId>) => void) => {
    const next = new Set(store.getState().openIds);
    mutator(next);
    store.setState({ openIds: next });
  };

  const api: PopupsApi = {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),
    open: (id) => withOpen((ids) => ids.add(id)),
    close: (id) => withOpen((ids) => ids.delete(id)),
    closeAll: () => store.setState({ openIds: new Set() }),
    isOpen: (id) => store.getState().openIds.has(id),
  };
  return api;
}
