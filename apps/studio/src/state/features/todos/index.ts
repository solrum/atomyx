import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type { TodoScanner } from "../../../domain/features/todos/index.js";
import type { TodosApi, TodosSnapshot, TodoHit } from "./todos.contract.js";
import { createZustandTodos } from "./todos.zustand.js";

export type { TodosApi, TodosSnapshot, TodoHit };

export const TODOS_KEY = "todos";

export function createTodos(deps: {
  scanner: TodoScanner;
  getWorkspacePath: () => string | null;
}): TodosApi {
  return createZustandTodos(deps);
}

export function useTodos(): TodosSnapshot &
  Pick<TodosApi, "refresh" | "clear"> {
  const api = getFeature<TodosApi>(TODOS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return { ...snap, refresh: api.refresh, clear: api.clear };
}
