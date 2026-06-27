import { createStore } from "zustand/vanilla";
import type { StudioRuntime } from "../../../domain/features/runtime/index.js";
import { getFeature } from "../../core/registry.js";
import type { IosAgentApi } from "../ios-agent/index.js";
import { IOS_AGENT_KEY } from "../ios-agent/index.js";
import type { DevicesApi, DevicesSnapshot } from "./devices.contract.js";

interface DevicesFactoryDeps {
  readonly runtime: StudioRuntime;
}

/**
 * Zustand-backed implementation of `DevicesApi`. The factory is
 * the only symbol exported from the public surface — the store
 * itself is invisible to callers, so swapping state managers
 * requires changing only this file plus the factory wire-up.
 */
export function createZustandDevices(deps: DevicesFactoryDeps): DevicesApi {
  const store = createStore<DevicesSnapshot>(() => ({
    devices: [],
    selectedId: null,
    loading: false,
    error: null,
  }));

  const ensureAgent = (device: {
    readonly id: string;
    readonly platform: "ios" | "android";
    readonly kind: "simulator" | "emulator" | "device";
  }): void => {
    if (device.platform !== "ios") return;
    const agentKind = device.kind === "simulator" ? "simulator" : "device";
    void getFeature<IosAgentApi>(IOS_AGENT_KEY)
      .ensure(device.id, agentKind)
      .catch((err) => {
        console.error("[devices] iosAgent.ensure failed", err);
      });
  };

  const refresh = async (): Promise<void> => {
    store.setState({ loading: true, error: null });
    try {
      const devices = await deps.runtime.listDevices();
      const { selectedId } = store.getState();
      const nextSelectedId = devices.some((d) => d.id === selectedId)
        ? selectedId
        : (devices[0]?.id ?? null);
      store.setState({ devices, selectedId: nextSelectedId, loading: false });
      if (nextSelectedId !== null) {
        const picked = devices.find((d) => d.id === nextSelectedId);
        if (picked) ensureAgent(picked);
      }
    } catch (err) {
      store.setState({
        loading: false,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  };

  const select = async (id: string | null): Promise<void> => {
    if (id === null) {
      store.setState({ selectedId: null });
      return;
    }
    const device = store.getState().devices.find((d) => d.id === id);
    if (!device) return;
    store.setState({ selectedId: id });
    ensureAgent(device);
  };

  return {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),
    refresh,
    select,
  };
}
