export type AndroidAgentState = "idle" | "installing" | "ready" | "failed";

export interface AndroidAgentStatus {
  readonly serial: string;
  readonly state: AndroidAgentState;
  readonly message?: string;
  readonly port: number;
}

export interface AndroidAgentPort {
  ensure(serial: string): Promise<AndroidAgentStatus>;
  status(serial: string): Promise<AndroidAgentStatus>;
}
