/**
 * Action history for an Atomyx session. Per-context — instances are passed
 * through AtomyxContext, not used as a global singleton.
 */

import type { RawElement } from "../adapters/device-controller.port.js";

export interface HistoryEntry {
  index: number;
  timestamp: number;
  action: string;
  args: Record<string, unknown>;
  status: "ok" | "error";
  error?: string;
  durationMs: number;
  screenshotPath?: string;
  treeAfter?: RawElement;
}

export class HistoryStore {
  private entries: HistoryEntry[] = [];
  private startedAt: number | null = null;

  start() {
    this.entries = [];
    this.startedAt = Date.now();
  }

  push(entry: Omit<HistoryEntry, "index" | "timestamp">) {
    this.entries.push({
      index: this.entries.length,
      timestamp: Date.now(),
      ...entry,
    });
  }

  all(): HistoryEntry[] {
    return [...this.entries];
  }

  last(): HistoryEntry | undefined {
    return this.entries[this.entries.length - 1];
  }

  count(): number {
    return this.entries.length;
  }

  startedAtMs(): number | null {
    return this.startedAt;
  }

  clear() {
    this.entries = [];
    this.startedAt = null;
  }
}

