import { createStore } from "zustand/vanilla";
import type { StudioRuntime } from "../../../domain/features/runtime/index.js";
import type {
  RuntimeStatusApi,
  RuntimeStatusSnapshot,
} from "./runtime-status.contract.js";

export function createZustandRuntimeStatus(deps: {
  readonly runtime: StudioRuntime;
}): RuntimeStatusApi {
  const store = createStore<RuntimeStatusSnapshot>(() => ({
    status: "connecting",
    lastOk: null,
    lastError: null,
  }));

  const ping = async (): Promise<void> => {
    try {
      await deps.runtime.connect();
      store.setState({
        status: "connected",
        lastOk: Date.now(),
        lastError: null,
      });
    } catch (err) {
      store.setState({
        status: "disconnected",
        lastError: err instanceof Error ? err.message : String(err),
      });
    }
  };

  return {
    getSnapshot: () => store.getState(),
    subscribe: (l) => store.subscribe(l),
    ping,
  };
}
