import { type ChildProcess, spawn, execFile } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import net from "node:net";

/**
 * Manages the XCUITest driver process lifecycle — build-if-needed
 * → spawn `xcodebuild test-without-building` → wait for TCP port
 * → hand off to `IosDriver.connect()`.
 *
 * Design constraints:
 *
 *   - **detached: false** — the xcodebuild child is tied to the
 *     Node process. When Node exits, the child receives SIGHUP
 *     and terminates. No orphan management, no PID files, no
 *     lock files.
 *
 *   - **No xcodegen invocation.** xcodegen mutates the project
 *     directory and is surprising for library consumers. If the
 *     .xcodeproj is missing we throw with an actionable message
 *     pointing at `make setup`.
 *
 *   - **Reuse-first.** If the port already has an Atomyx driver
 *     responding to ping, we skip everything. Covers `make serve`
 *     in another terminal and idempotent re-connects.
 */

export class XctestLauncherError extends Error {
  constructor(
    message: string,
    public readonly code:
      | "no-xcode"
      | "no-project"
      | "build-failed"
      | "signing"
      | "launch-timeout"
      | "spawn-exit"
      | "sim-not-booted",
  ) {
    super(message);
    this.name = "XctestLauncherError";
  }
}

export interface XctestLauncherOptions {
  readonly udid: string;
  readonly kind: "simulator" | "device";
  readonly port: number;
  /** Path to `native/ios-driver/` (auto-detected from repo root if omitted). */
  readonly projectDir?: string;
  /** Apple Development Team ID. Required for `kind: "device"`. */
  readonly devTeam?: string;
  /** Budget for `xcodebuild build-for-testing`. Default 120s. */
  readonly buildTimeoutMs?: number;
  /** Budget for port to become reachable after spawn. Default 90s. */
  readonly launchTimeoutMs?: number;
  /** Optional log sink. */
  readonly log?: (msg: string) => void;
}

export class XctestLauncher {
  private child: ChildProcess | null = null;
  private readonly log: (msg: string) => void;

  constructor(private readonly opts: XctestLauncherOptions) {
    this.log = opts.log ?? (() => {});
  }

  /**
   * Ensure the XCUITest driver is running on `opts.port`. Detects
   * a pre-existing driver, builds if needed, spawns if needed,
   * waits for the port to answer a ping handshake.
   *
   * Safe to call multiple times — idempotent.
   */
  async ensureRunning(): Promise<void> {
    // 1. Probe: is a driver already listening?
    if (await probeDriverPing(this.opts.port)) {
      this.log("driver already running, reusing");
      return;
    }

    // 2. Resolve project directory.
    const projectDir = this.resolveProjectDir();
    const xcodeproj = path.join(projectDir, "AtomyxDriver.xcodeproj");
    if (!fs.existsSync(xcodeproj)) {
      throw new XctestLauncherError(
        `AtomyxDriver.xcodeproj not found at ${xcodeproj}. ` +
          `Run \`cd ${projectDir} && make setup\` first.`,
        "no-project",
      );
    }

    // 3. Check if build products exist; build if not.
    const config =
      this.opts.kind === "device"
        ? "Debug-iphoneos"
        : "Debug-iphonesimulator";
    const runnerApp = path.join(
      projectDir,
      "build",
      "Build",
      "Products",
      config,
      "AtomyxDriverUITests-Runner.app",
    );

    if (!fs.existsSync(runnerApp) || this.isSourceNewer(projectDir, runnerApp)) {
      this.log("build products missing or stale, building...");
      await this.build(projectDir);
    }

    // 4. Spawn xcodebuild test-without-building.
    this.log("spawning xcodebuild test-without-building...");
    await this.spawnDriver(projectDir);

    // 5. Wait for port to answer ping.
    await this.waitForPort();
    this.log("driver is ready");
  }

  /**
   * Shut down the managed xcodebuild child. No-op if we did not
   * spawn one (i.e. we reused a pre-existing driver).
   */
  async shutdown(): Promise<void> {
    if (!this.child) return;
    const proc = this.child;
    this.child = null;

    try {
      proc.kill("SIGTERM");
    } catch {
      // already dead
    }

    // Wait up to 5s for clean exit.
    await new Promise<void>((resolve) => {
      const timeout = setTimeout(() => {
        try {
          proc.kill("SIGKILL");
        } catch {
          // ignore
        }
        resolve();
      }, 5_000);
      proc.once("exit", () => {
        clearTimeout(timeout);
        resolve();
      });
    });
  }

  /** True if this launcher spawned a child that is still alive. */
  isRunning(): boolean {
    return this.child !== null && !this.child.killed;
  }

  // ── Private ──────────────────────────────────────────────────

  private resolveProjectDir(): string {
    if (this.opts.projectDir) {
      return this.opts.projectDir;
    }

    // Check env var.
    const fromEnv = process.env.ATOMYX_IOS_DRIVER_DIR;
    if (fromEnv && fs.existsSync(fromEnv)) {
      return fromEnv;
    }

    // Walk up from this file to find native/ios-driver/.
    // Package layout: packages/core-driver-ios/dist/xctest-launcher.js
    // → repo root is ../../..
    let dir = path.dirname(new URL(import.meta.url).pathname);
    for (let i = 0; i < 6; i++) {
      const candidate = path.join(dir, "native", "ios-driver");
      if (fs.existsSync(candidate)) return candidate;
      dir = path.dirname(dir);
    }

    throw new XctestLauncherError(
      "iOS driver project directory not found. Set ATOMYX_IOS_DRIVER_DIR " +
        "env var or pass projectDir option.",
      "no-project",
    );
  }

  /**
   * Check if any Swift source file is newer than the build product.
   * A rough mtime comparison — good enough to catch obvious edits.
   */
  private isSourceNewer(projectDir: string, runnerApp: string): boolean {
    try {
      const productMtime = fs.statSync(runnerApp).mtimeMs;
      const srcDirs = [
        path.join(projectDir, "Tests"),
        path.join(projectDir, "Sources"),
      ];

      for (const dir of srcDirs) {
        if (!fs.existsSync(dir)) continue;
        const files = fs.readdirSync(dir, { recursive: true }) as string[];
        for (const file of files) {
          if (!file.endsWith(".swift")) continue;
          const fileMtime = fs.statSync(path.join(dir, file)).mtimeMs;
          if (fileMtime > productMtime) return true;
        }
      }
    } catch {
      // If we can't stat, assume rebuild needed.
      return true;
    }
    return false;
  }

  private destination(): string {
    return this.opts.kind === "device"
      ? `platform=iOS,id=${this.opts.udid}`
      : `platform=iOS Simulator,id=${this.opts.udid}`;
  }

  private signOverrides(): string[] {
    if (this.opts.kind !== "device") return [];
    const team = this.opts.devTeam;
    if (!team) {
      throw new XctestLauncherError(
        "devTeam is required for physical device builds. " +
          "Set ATOMYX_DEV_TEAM env var or pass devTeam option.",
        "signing",
      );
    }
    return [
      "CODE_SIGN_STYLE=Automatic",
      "CODE_SIGNING_REQUIRED=YES",
      "CODE_SIGNING_ALLOWED=YES",
      `DEVELOPMENT_TEAM=${team}`,
    ];
  }

  private async build(projectDir: string): Promise<void> {
    const args = [
      "build-for-testing",
      "-project",
      "AtomyxDriver.xcodeproj",
      "-scheme",
      "AtomyxDriver",
      "-destination",
      this.destination(),
      "-derivedDataPath",
      "./build",
      ...this.signOverrides(),
    ];

    const timeout = this.opts.buildTimeoutMs ?? 120_000;
    this.log(`xcodebuild ${args[0]} (timeout ${timeout / 1000}s)`);

    try {
      await new Promise<void>((resolve, reject) => {
        execFile(
          "xcodebuild",
          args,
          {
            cwd: projectDir,
            timeout,
            maxBuffer: 10 * 1024 * 1024, // 10MB — xcodebuild is verbose
          },
          (err, _stdout, stderr) => {
            if (err) {
              // Detect signing-specific failures.
              if (
                stderr.includes("requires a development team") ||
                stderr.includes("No profiles were found") ||
                stderr.includes("Signing for")
              ) {
                reject(
                  new XctestLauncherError(
                    "Xcode code signing not configured. Set ATOMYX_DEV_TEAM " +
                      "env var or pass devTeam to IosDriverOptions.\n\n" +
                      `xcodebuild stderr (last 500 chars):\n${stderr.slice(-500)}`,
                    "signing",
                  ),
                );
                return;
              }
              // Generic build failure.
              const tail = stderr.slice(-800) || "(no stderr)";
              reject(
                new XctestLauncherError(
                  `xcodebuild build-for-testing failed (exit code ${(err as NodeJS.ErrnoException).code ?? "unknown"}).\n` +
                    `Run \`cd ${projectDir} && make setup\` to diagnose.\n\n` +
                    `stderr (last 800 chars):\n${tail}`,
                  "build-failed",
                ),
              );
              return;
            }
            resolve();
          },
        );
      });
    } catch (err) {
      if (err instanceof XctestLauncherError) throw err;
      const e = err as NodeJS.ErrnoException;
      if (e.code === "ENOENT") {
        throw new XctestLauncherError(
          "xcodebuild not found. Install Xcode from the App Store.",
          "no-xcode",
        );
      }
      throw err;
    }
  }

  private async spawnDriver(projectDir: string): Promise<void> {
    const args = [
      "test-without-building",
      "-project",
      "AtomyxDriver.xcodeproj",
      "-scheme",
      "AtomyxDriver",
      "-destination",
      this.destination(),
      "-derivedDataPath",
      "./build",
      "-only-testing:AtomyxDriverUITests/AtomyxDriverUITests/testServeCommands",
      ...this.signOverrides(),
    ];

    const proc = spawn("xcodebuild", args, {
      cwd: projectDir,
      stdio: ["ignore", "pipe", "pipe"],
      detached: false,
    });

    let stderrBuf = "";
    proc.stderr?.on("data", (chunk: Buffer) => {
      stderrBuf += chunk.toString();
      // Cap buffer at 10KB to avoid memory leak.
      if (stderrBuf.length > 10_000) {
        stderrBuf = stderrBuf.slice(-5_000);
      }
    });

    // Detect early exit (ENOENT, bad args, etc.).
    const earlyExitPromise = new Promise<never>((_resolve, reject) => {
      proc.once("error", (err: NodeJS.ErrnoException) => {
        if (err.code === "ENOENT") {
          reject(
            new XctestLauncherError(
              "xcodebuild not found. Install Xcode from the App Store.",
              "no-xcode",
            ),
          );
          return;
        }
        reject(
          new XctestLauncherError(
            `xcodebuild spawn failed: ${err.message}`,
            "spawn-exit",
          ),
        );
      });
      proc.once("exit", (code) => {
        reject(
          new XctestLauncherError(
            `xcodebuild test-without-building exited early (code ${code}). ` +
              `stderr: ${stderrBuf.trim().slice(-500) || "(empty)"}`,
            "spawn-exit",
          ),
        );
      });
    });

    // Race: either the port comes up, or xcodebuild dies trying.
    try {
      await Promise.race([this.waitForPort(), earlyExitPromise]);
    } catch (err) {
      if (!proc.killed) proc.kill("SIGTERM");
      throw err;
    }

    // Success — swap to long-running listeners.
    proc.removeAllListeners("exit");
    proc.removeAllListeners("error");
    proc.on("exit", (code) => {
      if (this.child === proc) {
        process.stderr.write(
          `[atomyx/ios] xcodebuild exited (code ${code}) — driver stopped\n`,
        );
        this.child = null;
      }
    });
    this.child = proc;
  }

  private async waitForPort(): Promise<void> {
    const timeout = this.opts.launchTimeoutMs ?? 90_000;
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      if (await probeDriverPing(this.opts.port)) return;
      await new Promise((r) => setTimeout(r, 500));
    }
    throw new XctestLauncherError(
      `XCUITest driver did not start listening on port ${this.opts.port} ` +
        `within ${timeout / 1000}s. Check Xcode console for errors.`,
      "launch-timeout",
    );
  }
}

// ── Shared TCP probe utilities ────────────────────────────────
// Also used by Iproxy — exported so both can share.

/**
 * One-shot TCP connect with timeout. Returns true if a listener
 * is accepting connections on `127.0.0.1:port`.
 */
export function canConnect(port: number, timeoutMs: number): Promise<boolean> {
  return new Promise((resolve) => {
    const sock = net.createConnection({ host: "127.0.0.1", port });
    let done = false;
    const finish = (ok: boolean) => {
      if (done) return;
      done = true;
      sock.destroy();
      resolve(ok);
    };
    sock.once("connect", () => finish(true));
    sock.once("error", () => finish(false));
    setTimeout(() => finish(false), timeoutMs);
  });
}

/**
 * Handshake probe — opens a throwaway socket, sends a `ping`
 * and checks for `{ok:true, data:{pong:true}}`.
 */
export function probeDriverPing(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const sock = net.createConnection({ host: "127.0.0.1", port });
    let done = false;
    let buf = "";
    const finish = (ok: boolean) => {
      if (done) return;
      done = true;
      sock.removeAllListeners();
      sock.destroy();
      resolve(ok);
    };
    sock.setEncoding("utf8");
    sock.once("connect", () => {
      sock.write(JSON.stringify({ id: 0, type: "ping", args: {} }) + "\n");
    });
    sock.on("data", (chunk: string) => {
      buf += chunk;
      const nl = buf.indexOf("\n");
      if (nl < 0) return;
      try {
        const msg = JSON.parse(buf.slice(0, nl)) as {
          ok?: boolean;
          data?: { pong?: boolean };
        };
        finish(msg.ok === true && msg.data?.pong === true);
      } catch {
        finish(false);
      }
    });
    sock.once("error", () => finish(false));
    setTimeout(() => finish(false), 1000);
  });
}
