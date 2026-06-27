export type LogLevel = "debug" | "info" | "warn" | "error";

export type LogSource =
  | "sidecar"
  | "mirror"
  | "studio"
  | "ui"
  | "ios-agent"
  | string;

export interface LogEntry {
  readonly id: string;
  readonly ts: number;
  readonly source: LogSource;
  readonly level: LogLevel;
  readonly message: string;
}

export interface LogsPort {
  /**
   * Subscribe to backend log entries. Implementations push every
   * entry as it arrives; back-pressure is the consumer's
   * responsibility (state ring buffer drops oldest).
   *
   * Returns an unsubscribe function.
   */
  subscribe(listener: (entry: LogEntry) => void): () => void;
}
