export type IosAgentState = "idle" | "building" | "ready" | "failed";

export interface IosAgentStatus {
  readonly udid: string;
  readonly state: IosAgentState;
  readonly message?: string;
  readonly port: number;
}

export interface IosAgentPort {
  ensure(udid: string, kind: "simulator" | "device"): Promise<IosAgentStatus>;
  status(udid: string): Promise<IosAgentStatus>;
}
