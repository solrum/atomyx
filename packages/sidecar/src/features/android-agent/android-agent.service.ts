import {
  AndroidAgentLauncher,
  probeAndroidAgentHealth,
} from "@atomyx/android-driver";
import type { EventBus } from "../../infra/events/event-bus.js";
import type {
  AndroidAgentState,
  AndroidAgentStatus,
  EnsureAndroidAgentParams,
} from "./android-agent.types.js";

const DEFAULT_PORT = 8765;

interface AgentEntry {
  state: AndroidAgentState;
  message?: string;
  launcher: AndroidAgentLauncher;
  ensurePromise?: Promise<void>;
  port: number;
}

/**
 * Owns the Atomyx Agent APK lifecycle per connected `serial`.
 * Calls into `ensure()` are fire-and-forget — callers poll
 * `status()` or listen for `androidAgentStatus` events instead of
 * awaiting the provisioning pipeline on the dispatcher thread.
 */
export class AndroidAgentService {
  private readonly entries = new Map<string, AgentEntry>();
  private readonly events: EventBus;

  constructor(deps: { readonly events: EventBus }) {
    this.events = deps.events;
  }

  status(serial: string): AndroidAgentStatus {
    const entry = this.entries.get(serial);
    if (!entry) {
      return { serial, state: "idle", port: DEFAULT_PORT };
    }
    return {
      serial,
      state: entry.state,
      message: entry.message,
      port: entry.port,
    };
  }

  ensure(params: EnsureAndroidAgentParams): AndroidAgentStatus {
    const { serial } = params;
    let entry = this.entries.get(serial);
    if (entry && (entry.state === "ready" || entry.state === "installing")) {
      return this.status(serial);
    }

    const launcher = new AndroidAgentLauncher({
      serial,
      hostPort: DEFAULT_PORT,
      devicePort: DEFAULT_PORT,
      apkPath: process.env.ATOMYX_ANDROID_APK,
    });

    entry = {
      state: "installing",
      launcher,
      port: DEFAULT_PORT,
    };
    this.entries.set(serial, entry);
    this.emit(serial);

    entry.ensurePromise = (async () => {
      try {
        if (await probeAndroidAgentHealth(DEFAULT_PORT)) {
          this.markReady(serial);
          return;
        }
        await launcher.ensureRunning();
        this.markReady(serial);
      } catch (err) {
        this.markFailed(serial, errorMessage(err));
      }
    })();

    return this.status(serial);
  }

  async dispose(): Promise<void> {
    this.entries.clear();
  }

  private markReady(serial: string): void {
    const entry = this.entries.get(serial);
    if (!entry) return;
    entry.state = "ready";
    entry.message = undefined;
    this.emit(serial);
  }

  private markFailed(serial: string, message: string): void {
    const entry = this.entries.get(serial);
    if (!entry) return;
    entry.state = "failed";
    entry.message = message;
    this.emit(serial);
  }

  private emit(serial: string): void {
    this.events.emit({
      event: "androidAgentStatus",
      payload: this.status(serial),
    });
  }
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
