import { createStore } from "zustand/vanilla";
import type {
  NavHistoryApi,
  NavHistorySnapshot,
  NavLocation,
} from "./nav-history.contract.js";

const HISTORY_CAP = 50;
const MIN_LINE_DELTA = 5;
const MIN_TIME_DELTA_MS = 250;

function shouldCoalesce(
  last: NavLocation | undefined,
  next: Omit<NavLocation, "timestamp">,
  now: number,
): boolean {
  if (!last) return false;
  if (last.path !== next.path) return false;
  if (now - last.timestamp < MIN_TIME_DELTA_MS) return true;
  return Math.abs(last.line - next.line) < MIN_LINE_DELTA;
}

export function createZustandNavHistory(): NavHistoryApi {
  const store = createStore<NavHistorySnapshot>(() => ({
    entries: [],
    cursor: -1,
    suppress: false,
  }));

  return {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    record(location) {
      const { entries, cursor, suppress } = store.getState();
      if (suppress) return;
      const now = Date.now();
      const last = entries[cursor];
      if (shouldCoalesce(last, location, now)) {
        const replaced = entries.slice(0, cursor);
        replaced.push({ ...location, timestamp: now });
        store.setState({ entries: replaced, cursor: replaced.length - 1 });
        return;
      }
      const trimmed = entries.slice(0, cursor + 1);
      trimmed.push({ ...location, timestamp: now });
      const overflow = Math.max(0, trimmed.length - HISTORY_CAP);
      const next = overflow > 0 ? trimmed.slice(overflow) : trimmed;
      store.setState({ entries: next, cursor: next.length - 1 });
    },

    back() {
      const { entries, cursor } = store.getState();
      if (cursor <= 0) return null;
      const nextCursor = cursor - 1;
      store.setState({ cursor: nextCursor });
      return entries[nextCursor] ?? null;
    },

    forward() {
      const { entries, cursor } = store.getState();
      if (cursor < 0 || cursor >= entries.length - 1) return null;
      const nextCursor = cursor + 1;
      store.setState({ cursor: nextCursor });
      return entries[nextCursor] ?? null;
    },

    clear() {
      store.setState({ entries: [], cursor: -1 });
    },

    beginNavigation() {
      store.setState({ suppress: true });
    },

    endNavigation() {
      store.setState({ suppress: false });
    },
  };
}
