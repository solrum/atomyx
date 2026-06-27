import type { LogEntry, LogsPort } from "./logs.port.js";

export class MockLogsPort implements LogsPort {
  private readonly listeners = new Set<(entry: LogEntry) => void>();

  subscribe(listener: (entry: LogEntry) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  emit(entry: LogEntry): void {
    for (const l of this.listeners) l(entry);
  }
}
