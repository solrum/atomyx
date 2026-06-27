export type IosAgentState =
  | "idle"
  | "building"
  | "ready"
  | "failed";

export interface IosAgentStatus {
  readonly udid: string;
  readonly state: IosAgentState;
  readonly message?: string;
  readonly port: number;
}

export interface EnsureIosAgentParams {
  readonly udid: string;
  readonly kind: "simulator" | "device";
}

export type SimHidState = "idle" | "spawning" | "ready" | "failed";

export interface SimHidStatus {
  readonly udid: string;
  readonly state: SimHidState;
  readonly port?: number;
  readonly message?: string;
}
