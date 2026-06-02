import { useSyncExternalStore } from "react";

import type { AndroidAgentPort } from "../../../domain/features/android-agent/android-agent.port.js";
import { getFeature } from "../../core/registry.js";
import type {
  AndroidAgentApi,
  AndroidAgentSnapshot,
  AndroidAgentStatus,
} from "./android-agent.contract.js";
import { createZustandAndroidAgent } from "./android-agent.zustand.js";

export type { AndroidAgentApi, AndroidAgentSnapshot, AndroidAgentStatus };

export const ANDROID_AGENT_KEY = "androidAgent";

export function createAndroidAgent(deps: {
  readonly port: AndroidAgentPort;
}): AndroidAgentApi {
  return createZustandAndroidAgent(deps);
}

export function useAndroidAgentStatus(
  serial: string | null,
): AndroidAgentStatus | null {
  const api = getFeature<AndroidAgentApi>(ANDROID_AGENT_KEY);
  const snapshot = useSyncExternalStore(
    api.subscribe,
    api.getSnapshot,
    api.getSnapshot,
  );
  if (!serial) return null;
  return snapshot.bySerial[serial] ?? null;
}
