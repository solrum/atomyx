import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import {
  XctestLauncher,
  probeDriverPing,
} from "@atomyx/ios-driver";
import type { EventBus } from "../../infra/events/event-bus.js";
import type {
  EnsureIosAgentParams,
  IosAgentState,
  IosAgentStatus,
  SimHidState,
  SimHidStatus,
} from "./ios-agent.types.js";

const DEFAULT_PORT = 22087;

interface AgentEntry {
  state: IosAgentState;
  message?: string;
  launcher: XctestLauncher;
  ensurePromise?: Promise<void>;
  kind: "simulator" | "device";
  port: number;
}

interface SimHidEntry {
  state: SimHidState;
  port?: number;
  message?: string;
  _child?: ReturnType<typeof spawn>;
  /** Resolves when state transitions to "ready" or "failed". */
  readyPromise: Promise<number>;
}

/**
 * Owns XCUITest agent process per (udid). Calls into ensure() are
 * fire-and-forget — caller polls status() or listens to events.
 *
 * The supervisor never blocks the JSON-RPC request queue: the
 * background spawn happens off the dispatcher thread.
 *
 * Also manages an optional atomyx-sim-hid helper per simulator UDID
 * for the streaming HID touch path. Use startSimHid(udid) to start
 * the helper and await the returned port; use simHidPort(udid) to
 * read the port after startup.
 */
export class IosAgentService {
  private readonly entries = new Map<string, AgentEntry>();
  private readonly simHidEntries = new Map<string, SimHidEntry>();
  private readonly events: EventBus;

  constructor(deps: { readonly events: EventBus }) {
    this.events = deps.events;
  }

  status(udid: string): IosAgentStatus {
    const entry = this.entries.get(udid);
    if (!entry) {
      return { udid, state: "idle", port: DEFAULT_PORT };
    }
    return {
      udid,
      state: entry.state,
      message: entry.message,
      port: entry.port,
    };
  }

  ensure(params: EnsureIosAgentParams): IosAgentStatus {
    const { udid, kind } = params;
    let entry = this.entries.get(udid);
    if (entry && (entry.state === "ready" || entry.state === "building")) {
      return this.status(udid);
    }

    const tag = `[ios-agent ${udid.slice(0, 8)}]`;
    const launcher = new XctestLauncher({
      udid,
      kind,
      port: DEFAULT_PORT,
      devTeam: process.env.ATOMYX_DEV_TEAM,
      projectDir: process.env.ATOMYX_IOS_DRIVER_DIR,
      log: (msg) => {
        process.stderr.write(`${tag} ${msg}\n`);
      },
    });

    entry = {
      state: "building",
      launcher,
      kind,
      port: DEFAULT_PORT,
    };
    this.entries.set(udid, entry);
    process.stderr.write(
      `${tag} ensure kind=${kind} port=${DEFAULT_PORT} (building)\n`,
    );
    this.emit(udid);

    entry.ensurePromise = (async () => {
      try {
        if (await probeDriverPing(DEFAULT_PORT)) {
          process.stderr.write(
            `${tag} port ${DEFAULT_PORT} already responds — reusing\n`,
          );
          this.markReady(udid);
          return;
        }
        await launcher.ensureRunning();
        this.markReady(udid);
      } catch (err) {
        this.markFailed(udid, errorMessage(err));
      }
    })();

    return this.status(udid);
  }

  // ── sim-hid launcher ──────────────────────────────────────────

  simHidStatus(udid: string): SimHidStatus {
    const entry = this.simHidEntries.get(udid);
    if (!entry) return { udid, state: "idle" };
    return {
      udid,
      state: entry.state,
      port: entry.port,
      message: entry.message,
    };
  }

  simHidPort(udid: string): number | undefined {
    return this.simHidEntries.get(udid)?.port;
  }

  /**
   * Spawns the atomyx-sim-hid helper for the given simulator UDID if
   * not already running. Returns a Promise that resolves with the WS
   * port when the helper is ready, or rejects if it fails to start.
   *
   * The helper binary path is resolved via ATOMYX_SIM_HID_BIN env
   * var (for tests/dev) or from well-known relative paths next to
   * this process's executable (production Tauri build).
   *
   * State machine: idle → spawning → ready | failed.
   * Concurrent calls for the same UDID share the same promise.
   */
  startSimHid(udid: string): Promise<number> {
    const existing = this.simHidEntries.get(udid);
    if (
      existing &&
      (existing.state === "ready" || existing.state === "spawning")
    ) {
      return existing.readyPromise;
    }

    const tag = `[sim-hid ${udid.slice(0, 8)}]`;
    let resolveReady!: (port: number) => void;
    let rejectReady!: (err: Error) => void;
    const readyPromise = new Promise<number>((res, rej) => {
      resolveReady = res;
      rejectReady = rej;
    });

    const entry: SimHidEntry = { state: "spawning", readyPromise };
    this.simHidEntries.set(udid, entry);
    process.stderr.write(`${tag} spawning helper\n`);

    let helperPath: string;
    try {
      helperPath = resolveSimHidBinary();
    } catch (err) {
      entry.state = "failed";
      entry.message = errorMessage(err);
      process.stderr.write(
        `${tag} failed to resolve binary: ${entry.message}\n`,
      );
      rejectReady(new Error(entry.message));
      return readyPromise;
    }

    const child = spawn(helperPath, ["--udid", udid], {
      stdio: ["ignore", "pipe", "pipe"],
    });
    entry._child = child;

    let handshakeReceived = false;
    let stdoutBuf = "";

    child.stdout?.setEncoding("utf8");
    child.stdout?.on("data", (chunk: string) => {
      if (handshakeReceived) return;
      stdoutBuf += chunk;
      const nl = stdoutBuf.indexOf("\n");
      if (nl < 0) return;
      const line = stdoutBuf.slice(0, nl).trim();
      handshakeReceived = true;
      // Drain remaining stdout silently — helper is quiet after handshake.
      child.stdout?.resume();

      let hs: { event?: string; port?: number; transport?: string };
      try {
        hs = JSON.parse(line);
      } catch {
        const msg = `invalid handshake JSON: ${line}`;
        entry.state = "failed";
        entry.message = msg;
        process.stderr.write(`${tag} ${msg}\n`);
        child.kill("SIGTERM");
        rejectReady(new Error(msg));
        return;
      }

      if (hs.transport !== "ws-input" || typeof hs.port !== "number") {
        const msg = `unexpected handshake: ${line}`;
        entry.state = "failed";
        entry.message = msg;
        process.stderr.write(`${tag} ${msg}\n`);
        child.kill("SIGTERM");
        rejectReady(new Error(msg));
        return;
      }

      entry.state = "ready";
      entry.port = hs.port;
      process.stderr.write(`${tag} ready port=${hs.port}\n`);
      resolveReady(hs.port);
    });

    child.stderr?.setEncoding("utf8");
    child.stderr?.on("data", (chunk: string) => {
      for (const line of chunk.split("\n")) {
        const trimmed = line.trim();
        if (trimmed.length > 0) process.stderr.write(`${tag} ${trimmed}\n`);
      }
    });

    child.on("exit", (code, signal) => {
      if (entry.state !== "ready") {
        const msg = `helper exited early: code=${code} signal=${signal}`;
        entry.state = "failed";
        entry.message = msg;
        process.stderr.write(`${tag} ${msg}\n`);
        rejectReady(new Error(msg));
      } else {
        process.stderr.write(
          `${tag} helper exited code=${code} signal=${signal}\n`,
        );
        entry.state = "failed";
        entry.message = `exited: code=${code}`;
      }
    });

    child.on("error", (err) => {
      if (entry.state !== "ready") {
        entry.state = "failed";
        entry.message = err.message;
        process.stderr.write(`${tag} spawn error: ${err.message}\n`);
        rejectReady(err);
      }
    });

    return readyPromise;
  }

  async dispose(): Promise<void> {
    // Terminate XCUITest launchers.
    const all = [...this.entries.values()];
    this.entries.clear();
    await Promise.allSettled(all.map((e) => e.launcher.shutdown()));

    // Terminate sim-hid children.
    for (const [, entry] of this.simHidEntries) {
      entry._child?.kill("SIGTERM");
    }
    this.simHidEntries.clear();
  }

  private markReady(udid: string): void {
    const entry = this.entries.get(udid);
    if (!entry) return;
    entry.state = "ready";
    entry.message = undefined;
    this.emit(udid);
  }

  private markFailed(udid: string, message: string): void {
    const entry = this.entries.get(udid);
    if (!entry) return;
    entry.state = "failed";
    entry.message = message;
    this.emit(udid);
  }

  private emit(udid: string): void {
    this.events.emit({
      event: "iosAgentStatus",
      payload: this.status(udid),
    });
  }
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

/**
 * Resolves the path to the atomyx-sim-hid helper binary. Checks
 * `ATOMYX_SIM_HID_BIN` env var first (explicit override for tests
 * and packaged builds), then well-known relative paths matching the
 * Tauri bundle layout. Note: `ATOMYX_SIM_HID` is the opt-in toggle
 * (=`1` to engage the HID path) and is distinct from the binary
 * override — overloading one variable for both roles produced
 * `spawn 1 ENOENT` failures when the toggle value was passed as a
 * command path.
 */
function resolveSimHidBinary(): string {
  const envPath = process.env.ATOMYX_SIM_HID_BIN;
  if (envPath) return envPath;

  const bundleRelPath =
    "atomyx-sim-hid.app/Contents/MacOS/atomyx-sim-hid";

  const candidates = [
    // Tauri production: helper copied next to the app binary.
    path.join(path.dirname(process.execPath), bundleRelPath),
    // Dev: relative to the monorepo root (cwd when running via npm test).
    path.resolve(
      "apps/studio/src-tauri/helpers/atomyx-sim-hid",
      bundleRelPath,
    ),
    path.resolve("src-tauri/helpers/atomyx-sim-hid", bundleRelPath),
    path.resolve("helpers/atomyx-sim-hid", bundleRelPath),
  ];

  for (const p of candidates) {
    try {
      fs.accessSync(p, fs.constants.X_OK);
      return p;
    } catch {
      // Not found or not executable — try next.
    }
  }

  throw new Error(
    "atomyx-sim-hid helper not found. " +
      "Build it with `bash apps/studio/src-tauri/helpers/atomyx-sim-hid/build.sh` " +
      "or set ATOMYX_SIM_HID_BIN to the bundle executable path.",
  );
}
