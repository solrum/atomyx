import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  ActionDefinition,
  ActionHandler,
  ActionsApi,
  ActionsSnapshot,
} from "./actions.contract.js";
import { createZustandActions } from "./actions.zustand.js";
import { ACTION_DEFINITIONS } from "./actions.definitions.js";

export type { ActionDefinition, ActionHandler, ActionsApi, ActionsSnapshot };
export { ACTION_DEFINITIONS };

export const ACTIONS_KEY = "actions";

export function createActions(): ActionsApi {
  return createZustandActions();
}

export function useActions(): ActionsSnapshot &
  Pick<ActionsApi, "openPalette" | "closePalette" | "setQuery" | "execute"> {
  const api = getFeature<ActionsApi>(ACTIONS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    openPalette: api.openPalette,
    closePalette: api.closePalette,
    setQuery: api.setQuery,
    execute: api.execute,
  };
}

export function registerActionHandler(
  id: string,
  handler: ActionHandler,
): () => void {
  return getFeature<ActionsApi>(ACTIONS_KEY).registerHandler(id, handler);
}
