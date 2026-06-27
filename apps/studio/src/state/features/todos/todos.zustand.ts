import { createStore } from "zustand/vanilla";
import type { TodoScanner } from "../../../domain/features/todos/index.js";
import type { TodosApi, TodosSnapshot } from "./todos.contract.js";

interface TodosFactoryDeps {
  readonly scanner: TodoScanner;
  readonly getWorkspacePath: () => string | null;
}

export function createZustandTodos(deps: TodosFactoryDeps): TodosApi {
  const store = createStore<TodosSnapshot>(() => ({
    items: [],
    loading: false,
  }));

  return {
    getSnapshot: () => store.getState(),
    subscribe: (l) => store.subscribe(l),
    async refresh() {
      const path = deps.getWorkspacePath();
      if (!path) {
        store.setState({ items: [], loading: false });
        return;
      }
      store.setState({ loading: true });
      try {
        const hits = await deps.scanner.scan(path);
        store.setState({ items: hits, loading: false });
      } catch {
        store.setState({ loading: false });
      }
    },
    clear() {
      store.setState({ items: [] });
    },
  };
}
