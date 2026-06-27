import { createStore } from "zustand/vanilla";
import type { StudioRuntime } from "../../../domain/features/runtime/index.js";
import type {
  UiInspectorApi,
  UiInspectorSnapshot,
  UiNodePath,
} from "./ui-inspector.contract.js";

export interface UiInspectorDeps {
  readonly runtime: StudioRuntime;
  readonly autoRefresh?: {
    readonly enabled: boolean;
    readonly intervalMs: number;
  };
}

const MIN_AUTO_REFRESH_INTERVAL_MS = 2000;
const DEFAULT_AUTO_REFRESH_INTERVAL_MS = 5000;
const INTERACTION_PAUSE_MS = 1000;

const EMPTY: UiInspectorSnapshot = {
  tree: null,
  selectedPath: null,
  loading: false,
  error: null,
  capturedForDeviceId: null,
  capturedAt: null,
  showRaw: false,
  autoRefreshEnabled: false,
  autoRefreshIntervalMs: DEFAULT_AUTO_REFRESH_INTERVAL_MS,
  autoRefreshPaused: false,
};

function clampInterval(ms: number): number {
  if (!Number.isFinite(ms)) return DEFAULT_AUTO_REFRESH_INTERVAL_MS;
  return Math.max(MIN_AUTO_REFRESH_INTERVAL_MS, Math.floor(ms));
}

export function createZustandUiInspector(
  deps: UiInspectorDeps,
): UiInspectorApi {
  const { runtime } = deps;
  const initialIntervalMs = deps.autoRefresh
    ? clampInterval(deps.autoRefresh.intervalMs)
    : DEFAULT_AUTO_REFRESH_INTERVAL_MS;
  const initialEnabled = deps.autoRefresh?.enabled ?? false;

  const store = createStore<UiInspectorSnapshot>(() => ({
    ...EMPTY,
    autoRefreshEnabled: initialEnabled,
    autoRefreshIntervalMs: initialIntervalMs,
  }));

  let timerId: ReturnType<typeof setInterval> | null = null;
  let lastInteractionAt = 0;
  let pausedExposed = false;

  function stopTimer(): void {
    if (timerId !== null) {
      clearInterval(timerId);
      timerId = null;
    }
  }

  function startTimer(): void {
    stopTimer();
    const ms = store.getState().autoRefreshIntervalMs;
    timerId = setInterval(tick, ms);
  }

  function setPausedExposed(value: boolean): void {
    if (pausedExposed === value) return;
    pausedExposed = value;
    store.setState({ autoRefreshPaused: value });
  }

  function tick(): void {
    const snap = store.getState();
    if (!snap.autoRefreshEnabled) return;
    if (snap.loading) return;
    if (snap.capturedForDeviceId === null) return;

    const now = Date.now();
    const isPaused = now - lastInteractionAt < INTERACTION_PAUSE_MS;
    setPausedExposed(isPaused);
    if (isPaused) return;

    void api.refresh(snap.capturedForDeviceId);
  }

  const api: UiInspectorApi = {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    async refresh(deviceId) {
      store.setState({ loading: true, error: null });
      try {
        const tree = await runtime.getUiTree(deviceId);
        store.setState({
          tree,
          loading: false,
          error: null,
          capturedForDeviceId: deviceId,
          capturedAt: Date.now(),
        });
      } catch (err) {
        store.setState({
          loading: false,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    },

    select(path: UiNodePath | null) {
      store.setState({ selectedPath: path });
    },

    clear() {
      const cur = store.getState();
      store.setState({
        ...EMPTY,
        autoRefreshEnabled: cur.autoRefreshEnabled,
        autoRefreshIntervalMs: cur.autoRefreshIntervalMs,
        autoRefreshPaused: false,
      });
      pausedExposed = false;
    },

    setShowRaw(value: boolean) {
      if (store.getState().showRaw === value) return;
      store.setState({ showRaw: value });
    },

    setAutoRefreshEnabled(enabled: boolean) {
      if (store.getState().autoRefreshEnabled === enabled) return;
      store.setState({ autoRefreshEnabled: enabled });
      if (enabled) {
        startTimer();
      } else {
        stopTimer();
        setPausedExposed(false);
      }
    },

    setAutoRefreshInterval(intervalMs: number) {
      const clamped = clampInterval(intervalMs);
      if (store.getState().autoRefreshIntervalMs === clamped) return;
      store.setState({ autoRefreshIntervalMs: clamped });
      if (store.getState().autoRefreshEnabled) {
        startTimer();
      }
    },

    notifyInteraction() {
      lastInteractionAt = Date.now();
    },

    dispose() {
      stopTimer();
      setPausedExposed(false);
    },
  };

  if (initialEnabled) startTimer();

  return api;
}
