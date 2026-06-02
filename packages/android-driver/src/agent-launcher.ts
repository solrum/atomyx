import { execFile, type ExecFileException } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { promisify } from "node:util";

import { adbForward } from "./adb.js";

const execFileAsync = promisify(execFile);

/**
 * Brings the Atomyx Agent APK up on a connected device/emulator and
 * confirms it is answering HTTP. Pipeline:
 *
 *   1. `adb forward` (idempotent) so host can reach device port 8765.
 *   2. Probe `/health` — if already OK, short-circuit.
 *   3. Install the APK — path resolved from options, env, or a repo
 *      ascent looking for `platforms/android-agent/app/build/outputs`.
 *   4. Rebind the accessibility service — Android caches the old
 *      binding across APK reinstalls, so `delete` then `put` is
 *      required. See `platforms/android-agent/README.md`.
 *   5. Start the foreground service so the HTTP server actually
 *      listens.
 *   6. Poll `/health` until it reports `{ok:true,accessibilityConnected:true}`.
 *
 * Idempotent — calling `ensureRunning()` repeatedly is cheap when
 * the probe in step 2 already passes.
 */

const AGENT_PACKAGE = "dev.atomyx.agent";
const ACCESSIBILITY_SERVICE =
  `${AGENT_PACKAGE}/${AGENT_PACKAGE}.service.AtomyxAccessibilityService`;
const FOREGROUND_SERVICE =
  `${AGENT_PACKAGE}/.control.AtomyxForegroundService`;
const DEFAULT_DEVICE_PORT = 8765;
const DEFAULT_HOST_PORT = 8765;
const HEALTH_PROBE_TIMEOUT_MS = 1_000;
const REBIND_SETTLE_MS = 1_000;

export class AndroidAgentLauncherError extends Error {
  constructor(
    message: string,
    public readonly code:
      | "no-adb"
      | "no-apk"
      | "install-failed"
      | "rebind-failed"
      | "launch-timeout",
  ) {
    super(message);
    this.name = "AndroidAgentLauncherError";
  }
}

export interface AndroidAgentLauncherOptions {
  readonly serial: string;
  /** Host port to forward from (default 8765). */
  readonly hostPort?: number;
  /** Device port the APK's HTTP server listens on (default 8765). */
  readonly devicePort?: number;
  /** Absolute path to `app-debug.apk`. Auto-detected if omitted. */
  readonly apkPath?: string;
  /** Budget for `/health` to answer after service start. Default 30s. */
  readonly launchTimeoutMs?: number;
  /** Optional log sink. */
  readonly log?: (msg: string) => void;
}

export class AndroidAgentLauncher {
  private readonly log: (msg: string) => void;
  private readonly hostPort: number;
  private readonly devicePort: number;

  constructor(private readonly opts: AndroidAgentLauncherOptions) {
    this.log = opts.log ?? (() => {});
    this.hostPort = opts.hostPort ?? DEFAULT_HOST_PORT;
    this.devicePort = opts.devicePort ?? DEFAULT_DEVICE_PORT;
  }

  async ensureRunning(): Promise<void> {
    await adbForward(this.opts.serial, this.hostPort, this.devicePort);

    if (await probeAndroidAgentHealth(this.hostPort)) {
      this.log("agent already healthy, reusing");
      return;
    }

    const apk = this.resolveApkPath();
    if (!fs.existsSync(apk)) {
      throw new AndroidAgentLauncherError(
        `APK not found at ${apk}. ` +
          "Build it with `cd platforms/android-agent && ./gradlew :app:assembleDebug` " +
          "or set ATOMYX_ANDROID_APK env var.",
        "no-apk",
      );
    }

    this.log(`installing ${path.basename(apk)}...`);
    await this.adbInstall(apk);

    this.log("rebinding accessibility service...");
    await this.rebindAccessibility();

    this.log("starting foreground service...");
    await this.startForegroundService();

    await this.waitForHealth();
    this.log("agent is ready");
  }

  private resolveApkPath(): string {
    if (this.opts.apkPath) return this.opts.apkPath;

    const fromEnv = process.env.ATOMYX_ANDROID_APK;
    if (fromEnv) return fromEnv;

    const rel = path.join(
      "platforms",
      "android-agent",
      "app",
      "build",
      "outputs",
      "apk",
      "debug",
      "app-debug.apk",
    );

    const anchors: string[] = [];
    try {
      anchors.push(path.dirname(new URL(import.meta.url).pathname));
    } catch {
      // CJS bundle — import.meta.url unavailable.
    }
    if (process.argv[1]) anchors.push(path.dirname(process.argv[1]));
    anchors.push(process.cwd());

    for (const start of anchors) {
      let dir = start;
      for (let i = 0; i < 8; i++) {
        const candidate = path.join(dir, rel);
        if (fs.existsSync(candidate)) return candidate;
        const parent = path.dirname(dir);
        if (parent === dir) break;
        dir = parent;
      }
    }

    // Fallback — let the existsSync check report the first plausible
    // location so the error message is actionable.
    return path.join(process.cwd(), rel);
  }

  private async adbInstall(apk: string): Promise<void> {
    try {
      await execFileAsync("adb", [
        "-s",
        this.opts.serial,
        "install",
        "-r",
        apk,
      ]);
    } catch (err) {
      const e = err as ExecFileException & { stderr?: string };
      if (e.code === "ENOENT") {
        throw new AndroidAgentLauncherError(
          "adb not found on PATH. Install Android platform-tools.",
          "no-adb",
        );
      }
      throw new AndroidAgentLauncherError(
        `adb install failed: ${e.stderr?.trim() || e.message}`,
        "install-failed",
      );
    }
  }

  private async rebindAccessibility(): Promise<void> {
    try {
      await this.adbShell([
        "settings",
        "delete",
        "secure",
        "enabled_accessibility_services",
      ]);
      await sleep(REBIND_SETTLE_MS);
      await this.adbShell([
        "settings",
        "put",
        "secure",
        "enabled_accessibility_services",
        ACCESSIBILITY_SERVICE,
      ]);
      await sleep(REBIND_SETTLE_MS);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      throw new AndroidAgentLauncherError(
        `accessibility rebind failed: ${message}`,
        "rebind-failed",
      );
    }
  }

  private async startForegroundService(): Promise<void> {
    await this.adbShell([
      "am",
      "start-foreground-service",
      "-n",
      FOREGROUND_SERVICE,
    ]);
  }

  private async adbShell(argv: string[]): Promise<void> {
    await execFileAsync("adb", ["-s", this.opts.serial, "shell", ...argv]);
  }

  private async waitForHealth(): Promise<void> {
    const timeout = this.opts.launchTimeoutMs ?? 30_000;
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      if (await probeAndroidAgentHealth(this.hostPort)) return;
      await sleep(500);
    }
    throw new AndroidAgentLauncherError(
      `Android agent did not answer /health on port ${this.hostPort} ` +
        `within ${timeout / 1000}s.`,
      "launch-timeout",
    );
  }
}

/**
 * One-shot probe — returns true only when `/health` responds with
 * `{ok:true, accessibilityConnected:true}`. A missing connection
 * (accessibility disabled) is treated as "not ready" because every
 * tool call needs the service bound.
 */
export async function probeAndroidAgentHealth(
  hostPort: number,
): Promise<boolean> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), HEALTH_PROBE_TIMEOUT_MS);
  try {
    const res = await fetch(`http://127.0.0.1:${hostPort}/health`, {
      signal: controller.signal,
    });
    if (!res.ok) return false;
    const body = (await res.json()) as {
      ok?: boolean;
      accessibilityConnected?: boolean;
    };
    return body.ok === true && body.accessibilityConnected === true;
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
