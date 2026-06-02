import { invoke } from "@tauri-apps/api/core";
import type {
  IosAgentPort,
  IosAgentStatus,
} from "../../../domain/features/ios-agent/ios-agent.port.js";

export class TauriIosAgentPort implements IosAgentPort {
  async ensure(
    udid: string,
    kind: "simulator" | "device",
  ): Promise<IosAgentStatus> {
    return invoke<IosAgentStatus>("ios_agent_ensure", { udid, kind });
  }

  async status(udid: string): Promise<IosAgentStatus> {
    return invoke<IosAgentStatus>("ios_agent_status", { udid });
  }
}
