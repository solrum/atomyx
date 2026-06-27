export type RuntimeConnectivity = "connecting" | "connected" | "disconnected";

export interface RuntimeStatusSnapshot {
  readonly status: RuntimeConnectivity;
  readonly lastOk: number | null;
  readonly lastError: string | null;
}

export interface RuntimeStatusApi {
  getSnapshot(): RuntimeStatusSnapshot;
  subscribe(listener: () => void): () => void;
  ping(): Promise<void>;
}
