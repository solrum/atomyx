import type {
  LogEntry,
  LogLevel,
  LogSource,
} from "../../../domain/features/logs/logs.port.js";

export type { LogEntry, LogLevel, LogSource };

export interface LogsFilter {
  readonly source: LogSource | "all";
  readonly minLevel: LogLevel;
  readonly search: string;
}

export interface LogsSnapshot {
  readonly items: readonly LogEntry[];
  readonly filter: LogsFilter;
  readonly autoScroll: boolean;
  /**
   * Distinct sources observed so far. Filter dropdown reads it.
   */
  readonly knownSources: readonly LogSource[];
}

export interface LogsApi {
  getSnapshot(): LogsSnapshot;
  subscribe(listener: () => void): () => void;
  /**
   * Append an entry. Used by the platform port adapter and by the
   * UI console proxy. Drops oldest when the ring buffer is full.
   */
  append(entry: LogEntry): void;
  setFilter(patch: Partial<LogsFilter>): void;
  setAutoScroll(on: boolean): void;
  clear(): void;
}
