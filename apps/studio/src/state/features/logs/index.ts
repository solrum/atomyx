import { useMemo, useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  LogEntry,
  LogLevel,
  LogSource,
  LogsApi,
  LogsFilter,
  LogsSnapshot,
} from "./logs.contract.js";
import { applyLogsFilter, createZustandLogs } from "./logs.zustand.js";

export type { LogEntry, LogLevel, LogSource, LogsApi, LogsFilter, LogsSnapshot };

export const LOGS_KEY = "logs";

export function createLogs(): LogsApi {
  return createZustandLogs();
}

export interface UseLogsResult
  extends LogsSnapshot,
    Pick<LogsApi, "append" | "setFilter" | "setAutoScroll" | "clear"> {
  /** Entries after `filter` has been applied. Recomputed on snapshot change. */
  readonly filteredEntries: readonly LogEntry[];
}

export function useLogs(): UseLogsResult {
  const api = getFeature<LogsApi>(LOGS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  const filteredEntries = useMemo(() => applyLogsFilter(snap), [snap]);
  return {
    ...snap,
    filteredEntries,
    append: api.append,
    setFilter: api.setFilter,
    setAutoScroll: api.setAutoScroll,
    clear: api.clear,
  };
}
