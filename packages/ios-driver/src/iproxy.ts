import { type ChildProcess, spawn } from "node:child_process";
import { canConnect, probeDriverPing } from "./xctest-launcher.js";

/**
 * `iproxy` (libimobiledevice) tunnel lifecycle. The Swift driver
 * binds to `127.0.0.1:<port>` INSIDE the XCUITest process on the
 * device. Simulator shares the host network namespace so loopback
 * reaches the driver directly; a physical device needs a USB
 * tunnel mapping host:port ↔ device:port over usbmux.
 *
 * Correctness requires two guards:
 *
 *   1. **Port-conflict detection with probe handshake before
 *      spawn.** If `host:port` already has a listener and that
 *      listener answers our ping message, it's an existing Atomyx
 *      driver tunnel (either the same device from a prior connect
 *      in this process, or a `make serve-device` running in
 *      another terminal). Reuse it — do NOT spawn a duplicate
 *      iproxy. If it doesn't answer ping, it's a simulator driver
 *      collision and we refuse with an actionable error.
 *
 *   2. **Poll for real tunnel connectivity after spawn.** iproxy
 *      emits no "ready" signal. A naive `setTimeout(300ms)` will
 *      resolve "success" even if iproxy's background bind is
 *      still failing — subsequent TCP connects would hit a stale
 *      listener on the same port. Poll until a fresh TCP connect
 *      succeeds, or fail with a hard deadline.
 *
 * Simulator path: this module is NOT invoked. The Swift driver's
 * loopback listener is directly reachable from the host. Only
 * physical-device drivers construct an `Iproxy` instance.
 */

export class IproxyError extends Error {
  constructor(
    message: string,
    public readonly code: "not-installed" | "bind-conflict" | "tunnel-timeout" | "spawn-exit",
  ) {
    super(message);
    this.name = "IproxyError";
  }
}

export interface IproxyOptions {
  readonly udid: string;
  /** Host-side port (127.0.0.1:PORT the TCP client connects to). */
  readonly hostPort: number;
  /** Device-side port the Swift driver listens on. Usually same as hostPort. */
  readonly devicePort: number;
  /** Total budget for the tunnel to come up. Default 5s. */
  readonly tunnelUpTimeoutMs?: number;
}

export class Iproxy {
  private proc: ChildProcess | null = null;

  constructor(private readonly opts: IproxyOptions) {}

  /**
   * Start the tunnel. Returns successfully when the host port is
   * reachable AND confirmed-empty or confirmed-reusable. Throws
   * `IproxyError` on any of the known failure modes.
   */
  async start(): Promise<void> {
    // Guard 1: if the host port is already occupied, probe it
    // with a ping handshake. If it's an Atomyx driver, reuse the
    // existing tunnel.
    if (await canConnect(this.opts.hostPort, 300)) {
      if (await probeDriverPing(this.opts.hostPort)) {
        // Reusable — skip spawn.
        return;
      }
      throw new IproxyError(
        `Port ${this.opts.hostPort} on 127.0.0.1 already has a listener that is NOT an Atomyx driver. ` +
          `Likely a simulator driver from a concurrent session. ` +
          `To switch to a device session: Ctrl+C the simulator driver (or pkill -f "xcodebuild.*AtomyxDriver") ` +
          `before starting the device driver.`,
        "bind-conflict",
      );
    }

    // Spawn iproxy with the libimobiledevice 1.3+ syntax:
    //   iproxy <LOCAL_PORT>:<DEVICE_PORT> -u <UDID>
    const proc = spawn(
      "iproxy",
      [`${this.opts.hostPort}:${this.opts.devicePort}`, "-u", this.opts.udid],
      { stdio: ["ignore", "pipe", "pipe"] },
    );

    // Capture stderr for diagnostic purposes — iproxy logs to
    // stderr on signature errors and device-trust failures.
    let stderrBuf = "";
    proc.stderr?.on("data", (chunk: Buffer) => {
      stderrBuf += chunk.toString();
    });

    // Early-exit detection: if iproxy dies within the first
    // second of spawn, that's a setup failure (ENOENT, bad udid,
    // device not paired, etc.) — fail fast.
    const earlyExit = new Promise<never>((_resolve, reject) => {
      const onError = (err: NodeJS.ErrnoException) => {
        if (err.code === "ENOENT") {
          reject(
            new IproxyError(
              "iproxy not found on PATH. Install libimobiledevice (brew install libimobiledevice) and retry.",
              "not-installed",
            ),
          );
          return;
        }
        reject(new IproxyError(`iproxy spawn failed: ${err.message}`, "spawn-exit"));
      };
      const onExit = (code: number | null) => {
        reject(
          new IproxyError(
            `iproxy exited early (code ${code}). stderr: ${stderrBuf.trim() || "(empty)"}`,
            "spawn-exit",
          ),
        );
      };
      proc.once("error", onError);
      proc.once("exit", onExit);
    });

    // Guard 2: poll until the tunnel is actually reachable.
    const tunnelUp = this.waitForTunnelUp(this.opts.tunnelUpTimeoutMs ?? 5_000);

    try {
      await Promise.race([tunnelUp, earlyExit]);
    } catch (err) {
      // Clean up the child if it's still alive.
      if (!proc.killed) proc.kill("SIGTERM");
      throw err;
    }

    // Tunnel up. Swap the startup error listeners for long-
    // running ones that log but don't throw.
    proc.removeAllListeners("exit");
    proc.removeAllListeners("error");
    proc.on("exit", (code) => {
      if (this.proc === proc) {
        process.stderr.write(
          `[atomyx/drivers-ios] iproxy exited (code ${code}) — USB tunnel broken\n`,
        );
        this.proc = null;
      }
    });
    this.proc = proc;
  }

  /**
   * Tear down the tunnel. Best-effort — any failure to SIGTERM
   * the child is swallowed.
   */
  async stop(): Promise<void> {
    if (this.proc) {
      try {
        this.proc.kill("SIGTERM");
      } catch {
        // already dead
      }
      this.proc = null;
    }
  }

  private async waitForTunnelUp(timeoutMs: number): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      if (await canConnect(this.opts.hostPort, 300)) return;
      await new Promise((r) => setTimeout(r, 200));
    }
    throw new IproxyError(
      `iproxy tunnel did not become reachable on 127.0.0.1:${this.opts.hostPort} within ${timeoutMs}ms. ` +
        `Is the Swift driver actually running on the device? Start it with ` +
        `\`make serve-device\` in another terminal.`,
      "tunnel-timeout",
    );
  }
}

// canConnect and probeDriverPing imported from xctest-launcher.ts
