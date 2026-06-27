import type { AndroidAgentStatus } from "../../../domain/features/android-agent/android-agent.port.js";

export interface AndroidAgentSnapshot {
  readonly bySerial: Readonly<Record<string, AndroidAgentStatus>>;
}

export interface AndroidAgentApi {
  getSnapshot(): AndroidAgentSnapshot;
  subscribe(listener: () => void): () => void;
  ensure(serial: string): Promise<void>;
  refresh(serial: string): Promise<void>;
  startPolling(serial: string, intervalMs?: number): () => void;
}

export type { AndroidAgentStatus };
