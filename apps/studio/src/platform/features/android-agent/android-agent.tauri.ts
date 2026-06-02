import { invoke } from "@tauri-apps/api/core";
import type {
  AndroidAgentPort,
  AndroidAgentStatus,
} from "../../../domain/features/android-agent/android-agent.port.js";

export class TauriAndroidAgentPort implements AndroidAgentPort {
  async ensure(serial: string): Promise<AndroidAgentStatus> {
    return invoke<AndroidAgentStatus>("android_agent_ensure", { serial });
  }

  async status(serial: string): Promise<AndroidAgentStatus> {
    return invoke<AndroidAgentStatus>("android_agent_status", { serial });
  }
}
