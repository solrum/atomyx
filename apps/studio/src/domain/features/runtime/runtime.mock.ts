import type { StudioRuntime } from "./runtime.port.js";
import type {
  App,
  Device,
  RunEvent,
  RunOpts,
  UiTreeNode,
} from "./runtime.types.js";

/**
 * In-memory runtime used by UI and state tests — lets components
 * assert against predictable device / app / run behaviour without
 * spinning up a real Node sidecar.
 *
 * The constructor accepts a fixture so tests stay self-documenting:
 * each test writes its own expected devices / apps / run events.
 */
export interface MockRuntimeFixture {
  readonly devices?: readonly Device[];
  readonly appsByDevice?: Readonly<Record<string, readonly App[]>>;
  readonly runEventsByScript?: Readonly<Record<string, readonly RunEvent[]>>;
  readonly screenshots?: Readonly<Record<string, Uint8Array>>;
  readonly uiTreesByDevice?: Readonly<Record<string, UiTreeNode>>;
}

export class MockRuntime implements StudioRuntime {
  private connected = false;

  constructor(private readonly fixture: MockRuntimeFixture = {}) {}

  async connect(): Promise<void> {
    this.connected = true;
  }

  async disconnect(): Promise<void> {
    this.connected = false;
  }

  async listDevices(): Promise<readonly Device[]> {
    this.assertConnected();
    return this.fixture.devices ?? [];
  }

  async listApps(deviceId: string): Promise<readonly App[]> {
    this.assertConnected();
    return this.fixture.appsByDevice?.[deviceId] ?? [];
  }

  async *runScript(yaml: string, _opts: RunOpts): AsyncIterable<RunEvent> {
    this.assertConnected();
    const events = this.fixture.runEventsByScript?.[yaml] ?? [];
    for (const event of events) {
      yield event;
    }
  }

  async stop(): Promise<void> {
    /* fixture-based tests do not observe abort behavior */
  }

  async screenshot(deviceId: string): Promise<Uint8Array> {
    this.assertConnected();
    return this.fixture.screenshots?.[deviceId] ?? new Uint8Array();
  }

  async getUiTree(deviceId: string): Promise<UiTreeNode> {
    this.assertConnected();
    return (
      this.fixture.uiTreesByDevice?.[deviceId] ?? {
        attributes: {},
        children: [],
      }
    );
  }

  private assertConnected(): void {
    if (!this.connected) {
      throw new Error("MockRuntime: call connect() before other methods.");
    }
  }
}
