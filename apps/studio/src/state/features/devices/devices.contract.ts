import type { Device } from "../../../domain/features/runtime/index.js";

export interface DevicesSnapshot {
  readonly devices: readonly Device[];
  readonly selectedId: string | null;
  readonly loading: boolean;
  readonly error: string | null;
}

/**
 * Live device list + the user's current selection.
 *
 * Selection is held in the renderer; every action that needs a
 * device (listApps, runScript, screenshot) passes `selectedId`.
 * The sidecar holds its own `selectDevice` state per-session,
 * but this store is the source of truth for the UI's choice and
 * survives across multiple sidecar calls.
 */
export interface DevicesApi {
  getSnapshot(): DevicesSnapshot;
  subscribe(listener: () => void): () => void;
  refresh(): Promise<void>;
  select(id: string | null): Promise<void>;
}

export type { Device };
