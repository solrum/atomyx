import { createStore } from "zustand/vanilla";
import type { StudioRuntime } from "../../../domain/features/runtime/index.js";
import type { AppsApi, AppsSnapshot } from "./apps.contract.js";

interface AppsFactoryDeps {
  readonly runtime: StudioRuntime;
}

export function createZustandApps(deps: AppsFactoryDeps): AppsApi {
  const store = createStore<AppsSnapshot>(() => ({
    byDevice: {},
    loading: false,
    error: null,
  }));

  const refresh = async (deviceId: string): Promise<void> => {
    if (deviceId.length === 0) return;
    store.setState({ loading: true, error: null });
    try {
      const apps = await deps.runtime.listApps(deviceId);
      store.setState({
        byDevice: { ...store.getState().byDevice, [deviceId]: apps },
        loading: false,
      });
    } catch (err) {
      store.setState({
        loading: false,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  };

  return {
    getSnapshot: () => store.getState(),
    subscribe: (l) => store.subscribe(l),
    refresh,
  };
}
