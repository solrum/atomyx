import { createStore } from "zustand/vanilla";

import type { IosAgentPort } from "../../../domain/features/ios-agent/ios-agent.port.js";
import type {
  IosAgentApi,
  IosAgentSnapshot,
  IosAgentStatus,
} from "./ios-agent.contract.js";

const DEFAULT_POLL_MS = 2_000;

export function createZustandIosAgent(deps: {
  readonly port: IosAgentPort;
}): IosAgentApi {
  const store = createStore<IosAgentSnapshot>(() => ({ byUdid: {} }));

  const set = (udid: string, status: IosAgentStatus) => {
    store.setState((s) => ({
      byUdid: { ...s.byUdid, [udid]: status },
    }));
  };

  return {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),
    ensure: async (udid, kind) => {
      const status = await deps.port.ensure(udid, kind);
      set(udid, status);
    },
    refresh: async (udid) => {
      const status = await deps.port.status(udid);
      set(udid, status);
    },
    startPolling: (udid, intervalMs = DEFAULT_POLL_MS) => {
      let stopped = false;
      const tick = async () => {
        if (stopped) return;
        try {
          const status = await deps.port.status(udid);
          if (!stopped) set(udid, status);
        } catch {
          // ignore — keep polling
        }
        if (!stopped) timer = setTimeout(tick, intervalMs);
      };
      let timer: ReturnType<typeof setTimeout> = setTimeout(tick, 0);
      return () => {
        stopped = true;
        clearTimeout(timer);
      };
    },
  };
}
