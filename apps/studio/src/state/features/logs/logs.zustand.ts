import { createStore } from "zustand/vanilla";
import type {
  LogEntry,
  LogsApi,
  LogsFilter,
  LogsSnapshot,
} from "./logs.contract.js";

const RING_CAPACITY = 5_000;

const DEFAULT_FILTER: LogsFilter = {
  source: "all",
  minLevel: "debug",
  search: "",
};

export function createZustandLogs(): LogsApi {
  const store = createStore<LogsSnapshot>(() => ({
    items: [],
    filter: DEFAULT_FILTER,
    autoScroll: true,
    knownSources: [],
  }));

  const append = (entry: LogEntry): void => {
    const s = store.getState();
    const items =
      s.items.length >= RING_CAPACITY
        ? [...s.items.slice(s.items.length - RING_CAPACITY + 1), entry]
        : [...s.items, entry];
    const knownSources = s.knownSources.includes(entry.source)
      ? s.knownSources
      : [...s.knownSources, entry.source];
    store.setState({ items, knownSources });
  };

  return {
    getSnapshot: () => store.getState(),
    subscribe: (l) => store.subscribe(l),
    append,
    setFilter: (patch) => {
      store.setState({ filter: { ...store.getState().filter, ...patch } });
    },
    setAutoScroll: (on) => store.setState({ autoScroll: on }),
    clear: () => store.setState({ items: [], knownSources: [] }),
  };
}

const LEVEL_RANK: Record<string, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3,
};

export function applyLogsFilter(snapshot: LogsSnapshot): readonly LogEntry[] {
  const { source, minLevel, search } = snapshot.filter;
  const minRank = LEVEL_RANK[minLevel] ?? 0;
  const needle = search.trim().toLowerCase();
  return snapshot.items.filter((e) => {
    if (source !== "all" && e.source !== source) return false;
    if ((LEVEL_RANK[e.level] ?? 0) < minRank) return false;
    if (needle && !e.message.toLowerCase().includes(needle)) return false;
    return true;
  });
}

