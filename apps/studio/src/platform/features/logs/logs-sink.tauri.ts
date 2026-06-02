import { invoke } from "@tauri-apps/api/core";
import type { LogsSink } from "../../../domain/features/logs/logs-sink.port.js";

export class TauriLogsSink implements LogsSink {
  emit(entry: {
    readonly source: string;
    readonly level: "debug" | "info" | "warn" | "error";
    readonly message: string;
  }): void {
    try {
      void invoke("log_emit", entry);
    } catch {
      /* never let logging break the app */
    }
  }
}
