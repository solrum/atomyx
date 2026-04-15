/**
 * Runtime context for an Atomyx session. Holds all mutable + lifetime state
 * that handlers need. Replaces the previous singleton state stores.
 *
 * Created once at server startup. Tools receive this via the factory
 * and grab whatever they need (controller, history, results).
 */

import type { DeviceController } from "../adapters/device-controller.port.js";
import { HistoryStore } from "../state/history.js";
import { ResultStore } from "../state/results.js";

export interface RecordedAction {
  type: string;
  args: Record<string, unknown>;
  timestamp: number;
}

export interface AtomyxContext {
  /** Currently selected device controller. Null when no device selected. */
  controller: DeviceController | null;
  /** Action history for the current session. */
  history: HistoryStore;
  /** Test run + bug + finding store. */
  results: ResultStore;
  /** Snapshot store (named UI tree captures used by get_tree_diff). */
  snapshots: Map<string, unknown>;
  /** Recorded mutating actions, persisted by save_as_test_case. */
  recordedActions: RecordedAction[];
  /**
   * Invalidate the cached UI tree. Populated by ui.tools.ts; called by
   * mutating tools (tap, fill, swipe, etc) after they run so the next
   * find_element / get_ui_tree call sees a fresh dump.
   */
  invalidateUiCache: () => void;
  /**
   * Name of the most recently completed tool. Updated by the server
   * dispatcher after each call. Used by anti-pattern detectors that need
   * to know whether the previous call was the same tool or different.
   */
  lastToolName: string | null;
}

export function createAtomyxContext(): AtomyxContext {
  return {
    controller: null,
    history: new HistoryStore(),
    results: new ResultStore(),
    snapshots: new Map(),
    recordedActions: [],
    invalidateUiCache: () => {},
    lastToolName: null,
  };
}

/** Convenience guard used by handlers that require a connected device. */
export function requireController(ctx: AtomyxContext): DeviceController {
  if (!ctx.controller) {
    throw new Error("No device selected. Call select_device first.");
  }
  return ctx.controller;
}
