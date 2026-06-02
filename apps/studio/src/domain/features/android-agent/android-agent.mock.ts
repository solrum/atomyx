import type {
  AndroidAgentPort,
  AndroidAgentStatus,
} from "./android-agent.port.js";

export class MockAndroidAgentPort implements AndroidAgentPort {
  private next: AndroidAgentStatus = {
    serial: "",
    state: "idle",
    port: 8765,
  };

  async ensure(serial: string): Promise<AndroidAgentStatus> {
    this.next = { serial, state: "installing", port: 8765 };
    return this.next;
  }

  async status(serial: string): Promise<AndroidAgentStatus> {
    return { ...this.next, serial };
  }

  setState(s: AndroidAgentStatus): void {
    this.next = s;
  }
}
