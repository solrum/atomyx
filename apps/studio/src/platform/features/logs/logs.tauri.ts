import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import type {
  LogEntry,
  LogsPort,
} from "../../../domain/features/logs/logs.port.js";

interface WireLogEntry {
  readonly id?: string;
  readonly ts?: number;
  readonly source: string;
  readonly level: string;
  readonly message: string;
}

const LEVELS = new Set(["debug", "info", "warn", "error"]);

function normalize(raw: WireLogEntry): LogEntry {
  return {
    id: raw.id ?? Math.random().toString(36).slice(2),
    ts: typeof raw.ts === "number" ? raw.ts : Date.now(),
    source: raw.source,
    level: LEVELS.has(raw.level)
      ? (raw.level as LogEntry["level"])
      : "info",
    message: raw.message,
  };
}

/**
 * Subscribes to the Rust `logs://entry` Tauri event. Each event
 * payload is a single log line; the adapter normalizes shape and
 * fans out via the port listener contract.
 */
export class TauriLogsPort implements LogsPort {
  subscribe(listener: (entry: LogEntry) => void): () => void {
    let unlisten: UnlistenFn | null = null;
    let stopped = false;

    void listen<WireLogEntry>("logs://entry", (event) => {
      listener(normalize(event.payload));
    }).then((fn) => {
      if (stopped) {
        fn();
      } else {
        unlisten = fn;
      }
    });

    return () => {
      stopped = true;
      if (unlisten) unlisten();
    };
  }
}
