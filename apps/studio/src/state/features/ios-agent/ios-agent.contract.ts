import type { IosAgentStatus } from "../../../domain/features/ios-agent/ios-agent.port.js";

export interface IosAgentSnapshot {
  readonly byUdid: Readonly<Record<string, IosAgentStatus>>;
}

export interface IosAgentApi {
  getSnapshot(): IosAgentSnapshot;
  subscribe(listener: () => void): () => void;
  ensure(udid: string, kind: "simulator" | "device"): Promise<void>;
  refresh(udid: string): Promise<void>;
  startPolling(udid: string, intervalMs?: number): () => void;
}

export type { IosAgentStatus };
