import { createStore } from "zustand/vanilla";

import type { AndroidAgentPort } from "../../../domain/features/android-agent/android-agent.port.js";
import type {
  AndroidAgentApi,
  AndroidAgentSnapshot,
  AndroidAgentStatus,
} from "./android-agent.contract.js";

const DEFAULT_POLL_MS = 2_000;

export function createZustandAndroidAgent(deps: {
  readonly port: AndroidAgentPort;
}): AndroidAgentApi {
  const store = createStore<AndroidAgentSnapshot>(() => ({ bySerial: {} }));

  const set = (serial: string, status: AndroidAgentStatus) => {
    store.setState((s) => ({
      bySerial: { ...s.bySerial, [serial]: status },
    }));
  };

  return {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),
    ensure: async (serial) => {
      const status = await deps.port.ensure(serial);
      set(serial, status);
    },
    refresh: async (serial) => {
      const status = await deps.port.status(serial);
      set(serial, status);
    },
    startPolling: (serial, intervalMs = DEFAULT_POLL_MS) => {
      let stopped = false;
      const tick = async () => {
        if (stopped) return;
        try {
          const status = await deps.port.status(serial);
          if (!stopped) set(serial, status);
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
