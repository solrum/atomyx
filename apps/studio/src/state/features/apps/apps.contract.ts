import type { App } from "../../../domain/features/runtime/index.js";

export interface AppsSnapshot {
  /** Cached apps per device — lookups stay cheap across picker opens. */
  readonly byDevice: Readonly<Record<string, readonly App[]>>;
  readonly loading: boolean;
  readonly error: string | null;
}

export interface AppsApi {
  getSnapshot(): AppsSnapshot;
  subscribe(listener: () => void): () => void;
  refresh(deviceId: string): Promise<void>;
}

export type { App };
