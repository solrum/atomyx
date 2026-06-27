import type { IosAgentPort, IosAgentStatus } from "./ios-agent.port.js";

export class MockIosAgentPort implements IosAgentPort {
  private next: IosAgentStatus = {
    udid: "",
    state: "idle",
    port: 22087,
  };

  async ensure(
    udid: string,
    _kind: "simulator" | "device",
  ): Promise<IosAgentStatus> {
    this.next = { udid, state: "building", port: 22087 };
    return this.next;
  }

  async status(udid: string): Promise<IosAgentStatus> {
    return { ...this.next, udid };
  }

  setState(s: IosAgentStatus): void {
    this.next = s;
  }
}
