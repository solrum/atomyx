import { useSyncExternalStore } from "react";

import type { IosAgentPort } from "../../../domain/features/ios-agent/ios-agent.port.js";
import { getFeature } from "../../core/registry.js";
import type {
  IosAgentApi,
  IosAgentSnapshot,
  IosAgentStatus,
} from "./ios-agent.contract.js";
import { createZustandIosAgent } from "./ios-agent.zustand.js";

export type { IosAgentApi, IosAgentSnapshot, IosAgentStatus };

export const IOS_AGENT_KEY = "iosAgent";

export function createIosAgent(deps: { readonly port: IosAgentPort }): IosAgentApi {
  return createZustandIosAgent(deps);
}

export function useIosAgentStatus(udid: string | null): IosAgentStatus | null {
  const api = getFeature<IosAgentApi>(IOS_AGENT_KEY);
  const snapshot = useSyncExternalStore(
    api.subscribe,
    api.getSnapshot,
    api.getSnapshot,
  );
  if (!udid) return null;
  return snapshot.byUdid[udid] ?? null;
}
