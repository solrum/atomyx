import { createStore } from "zustand/vanilla";
import type { Problem, ProblemsApi, ProblemsSnapshot } from "./problems.contract.js";

export function createZustandProblems(): ProblemsApi {
  const store = createStore<ProblemsSnapshot>(() => ({ items: [] }));
  return {
    getSnapshot: () => store.getState(),
    subscribe: (l) => store.subscribe(l),
    set: (items: readonly Problem[]) => store.setState({ items }),
    clear: () => store.setState({ items: [] }),
  };
}
