import type { LogLevel, LogSource } from "./logs.port.js";

export interface LogsSink {
  /**
   * Forward a UI-side log line to the backend logging pipeline so
   * it lands in the same store as sidecar/mirror entries. Returns
   * synchronously; delivery is best-effort — a failing sink must
   * not throw into the caller.
   */
  emit(entry: {
    readonly source: LogSource;
    readonly level: LogLevel;
    readonly message: string;
  }): void;
}
