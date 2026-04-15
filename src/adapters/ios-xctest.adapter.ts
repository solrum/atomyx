import { type ChildProcess, execFile, spawn } from "node:child_process";
import net from "node:net";
import { promisify } from "node:util";
import type {
  ActionResult,
  CompactElement,
  DeviceController,
  ForegroundInfo,
  InstalledApp,
  KeyboardInfo,
  RawElement,
  ResolvedElement,
  Selector,
  TypeKeyboardResult,
} from "./device-controller.port.js";

/**
 * iOS adapter — Phase 2 Week 2 baseline.
 *
 * Implements the 3 commands frozen in Week 1 (`launchApp`, `dumpTree`,
 * `tapAt`) plus `currentForeground` and `dispose` over a line-delimited
 * JSON TCP protocol to the Swift driver in `native/ios-driver/`. All other
 * `DeviceController` methods throw — they're Phase 3 scope.
 *
 * Wire protocol (frozen — see docs/ios.md):
 *   → { id, type, args }
 *   ← { id, ok: true, data }   OR   { id, ok: false, error }
 *
 * Transport is TCP `127.0.0.1:22087`. The simulator shares its network
 * namespace with the host, so localhost reaches a port bound inside the
 * XCUITest process. Real-device support (usbmux tunneling) is Phase 5.
 *
 * Adapter-side mapping responsibilities (beyond what the Swift bridge
 * already filters):
 *   - **Selector dedupe**: drop `contentDesc` when it equals `resourceId`
 *     (Apple apps frequently set `accessibilityIdentifier` ==
 *     `accessibilityLabel`).
 *   - **Clickable derivation**: map to `true` only for known interactive
 *     element types OR hittable-with-identifier. `isHittable` alone is
 *     too loose (returns true for most non-obscured elements).
 *   - **State tracking**: `currentForeground()` returns the tracked
 *     bundleId set by `launchApp()`. See docs/ios.md Week 1 finding #1.
 */

const execFileAsync = promisify(execFile);

const DRIVER_HOST = "127.0.0.1";
const DRIVER_PORT = 22087;
const CONNECT_TIMEOUT_MS = 30_000;
const CONNECT_RETRY_MS = 500;

const INTERACTIVE_ROLES = new Set([
  "button",
  "cell",
  "link",
  "textField",
  "secureTextField",
  "searchField",
  "switch",
  "slider",
  "picker",
]);

interface WirePending {
  resolve: (data: Record<string, unknown>) => void;
  reject: (err: Error) => void;
}

interface DriverElement {
  type: string;
  id: string;
  label: string;
  value?: string;
  enabled: boolean;
  hittable: boolean;
  x: number;
  y: number;
  w: number;
  h: number;
}

type Bounds = { left: number; top: number; right: number; bottom: number };

function boundsEqual(a: Bounds, b: Bounds): boolean {
  return (
    a.left === b.left &&
    a.top === b.top &&
    a.right === b.right &&
    a.bottom === b.bottom
  );
}

function describeBounds(b: Bounds): string {
  return `[${b.left},${b.top},${b.right},${b.bottom}]`;
}

function clamp(value: number, min: number, max: number): number {
  if (max < min) return min;
  return Math.min(Math.max(value, min), max);
}

export class IosXctestController implements DeviceController {
  readonly platform = "ios" as const;
  readonly deviceId: string;

  private conn: net.Socket | null = null;
  private nextId = 1;
  private pending = new Map<number, WirePending>();
  private buffer = "";
  private lastLaunchedBundleId = "";
  /**
   * Connection port remembered after `connect()` so crash-recovery
   * `reconnect()` can re-open the socket without the caller passing
   * it again.
   */
  private port = DRIVER_PORT;
  /**
   * Set when the socket has fired `close` or `error` — subsequent
   * `call()` invocations should not queue into a dead pending map.
   */
  private connectionDead = false;
  /**
   * Simulator shares the host network namespace (TCP localhost
   * reaches the driver directly). Physical device requires an
   * `iproxy` (libimobiledevice) tunnel that bridges host:port ↔
   * device:port over USB. Tracked per-instance so `dispose()` knows
   * whether to kill the tunnel child process.
   */
  private kind: "sim" | "device" = "sim";
  private iproxyProc: ChildProcess | null = null;
  /**
   * Lazy per-session cache of the tracked app's screen size in
   * points. Fetched on first `ensureVisible` need, invalidated on
   * any state change that could alter the viewport: `launchApp`
   * (different app → different frame), `forceStopApp` (matching),
   * `reconnect` / `handleDisconnect` (driver state gone).
   *
   * Rotation isn't detected; documented as a known limitation. If
   * the agent rotates mid-session, call `launchApp` again to refresh.
   */
  private screenSize: { width: number; height: number } | null = null;

  private constructor(deviceId: string) {
    this.deviceId = deviceId;
  }

  static async connect(
    deviceId: string,
    port = DRIVER_PORT,
    kind: "sim" | "device" = "sim",
  ): Promise<IosXctestController> {
    const ctrl = new IosXctestController(deviceId);
    ctrl.port = port;
    ctrl.kind = kind;

    // Physical device: tunnel host:port ↔ device:port via iproxy
    // BEFORE attempting TCP connect. Simulator shares host network
    // namespace so no tunnel needed.
    if (kind === "device") {
      await ctrl.startIproxy();
    }

    await ctrl.waitForDriver(port);
    return ctrl;
  }

  /**
   * Spawn `iproxy <port> <port> <UDID>` (libimobiledevice) to bridge
   * host TCP port to the same port on the connected device over USB
   * via usbmux.
   *
   * Correctness requires TWO guards that earlier heuristic-only
   * implementations got wrong:
   *
   *   1. **Port-conflict detection before spawn.** If `this.port` on
   *      the host already has a listener (typically the simulator
   *      driver from a concurrent `make serve`), iproxy will fail to
   *      bind silently or with delayed exit. The adapter would then
   *      connect to THAT existing listener and happily route commands
   *      to the wrong process. Check free-port synchronously first
   *      and surface an actionable "stop the sim driver" error.
   *
   *   2. **Poll for real tunnel connectivity after spawn.** iproxy
   *      emits no "ready" signal. A naive
   *      `setTimeout(..., 300ms)` resolves "success" even if iproxy's
   *      background bind attempt is still failing, leaving the
   *      adapter to connect to a stale listener on the same port.
   *      Instead, poll `127.0.0.1:port` with short TCP connects until
   *      a successful connect confirms the tunnel is live, with a
   *      hard 5-second deadline.
   *
   * The child process runs until `dispose()` kills it. `SIGTERM` is
   * sent to allow graceful shutdown; iproxy respects it.
   */
  private async startIproxy(): Promise<void> {
    // Guard 1: if a listener is already up on this.port, handshake it
    // before deciding. An Atomyx driver that answers `ping` is almost
    // certainly a live tunnel from a prior connect in this process
    // (or a user-started `make serve-device`) — reuse it instead of
    // spawning a duplicate iproxy. Anything else (sim driver, unrelated
    // process) still hits the original collision error.
    if (await this.canConnect(this.port, 300)) {
      if (await this.probeDriverPing(this.port)) {
        process.stderr.write(
          `[ios-adapter] reusing existing driver listener on 127.0.0.1:${this.port}\n`,
        );
        return;
      }
      await this.ensurePortFree(this.port);
    }

    // libimobiledevice 1.3+ syntax: `iproxy LOCAL:REMOTE -u UDID`.
    // The earlier positional syntax (`iproxy LOCAL REMOTE UDID`) was
    // deprecated and new builds reject it with EINVAL (exit code 22).
    // Modern Homebrew ships 1.3.x so this is the correct form.
    const proc = spawn(
      "iproxy",
      [`${this.port}:${this.port}`, "-u", this.deviceId],
      { stdio: ["ignore", "pipe", "pipe"] },
    );

    // Collect stderr so argument parse errors / usbmuxd failures
    // get surfaced in the rejection message instead of being lost.
    let stderrBuf = "";
    proc.stderr?.setEncoding("utf8");
    proc.stderr?.on("data", (chunk: string) => {
      stderrBuf += chunk;
      if (stderrBuf.length > 2048) stderrBuf = stderrBuf.slice(-2048);
    });

    // Wire up error handlers BEFORE we await, so ENOENT / early exit
    // surfaces instead of hanging.
    const spawnError = new Promise<never>((_, reject) => {
      proc.once("error", (err) => {
        const code = (err as NodeJS.ErrnoException).code;
        reject(
          new Error(
            code === "ENOENT"
              ? "iproxy not found. Install libimobiledevice: `brew install libimobiledevice`"
              : `iproxy spawn failed: ${err.message}`,
          ),
        );
      });
      proc.once("exit", (exitCode) => {
        const stderrTail = stderrBuf.trim();
        reject(
          new Error(
            `iproxy exited (code ${exitCode}) before the tunnel came up.\n` +
              (stderrTail ? `iproxy stderr: ${stderrTail}\n` : "") +
              `Common causes:\n` +
              `  - libimobiledevice version too old (need 1.3+). Upgrade: brew upgrade libimobiledevice\n` +
              `  - Device ${this.deviceId} not connected via USB or not trusted\n` +
              `  - The driver isn't running on the device — start it with \`make serve-device\`\n` +
              `  - Port ${this.port} collision with another tool`,
          ),
        );
      });
    });

    this.iproxyProc = proc;

    // Guard 2: poll for real tunnel connectivity.
    try {
      await Promise.race([this.waitForTunnelUp(this.port, 5000), spawnError]);
    } catch (err) {
      // Cleanup on failure
      if (this.iproxyProc) {
        this.iproxyProc.kill("SIGTERM");
        this.iproxyProc = null;
      }
      throw err;
    }

    // Tunnel up. Swap the startup error listeners for long-running
    // ones that log but don't throw (we can't throw from an async
    // event listener outside a Promise context).
    proc.removeAllListeners("exit");
    proc.removeAllListeners("error");
    proc.on("exit", (code) => {
      if (this.iproxyProc === proc) {
        process.stderr.write(
          `[ios-adapter] iproxy exited (code ${code}) — USB tunnel broken\n`,
        );
        this.iproxyProc = null;
      }
    });
  }

  /**
   * Reject if `127.0.0.1:port` already has a listener. Used as a
   * guard before spawning iproxy so a stale sim driver on the same
   * port surfaces as a clear error instead of a silent route to the
   * wrong process.
   */
  private async ensurePortFree(port: number): Promise<void> {
    const occupied = await this.canConnect(port, 300);
    if (occupied) {
      throw new Error(
        `Port ${port} on 127.0.0.1 already has a listener — likely a simulator ` +
          `driver running via \`make serve\`. Only one driver can bind this ` +
          `port at a time. To switch to a device session:\n` +
          `  1. Stop the sim driver: pkill -f "xcodebuild.*AtomyxDriver" (or Ctrl+C its terminal)\n` +
          `  2. Start the device driver in another terminal: cd native/ios-driver && make serve-device\n` +
          `  3. Retry select_device`,
      );
    }
  }

  /**
   * Poll `127.0.0.1:port` with short TCP connects until one succeeds
   * or the deadline expires. Returns normally on success, throws on
   * deadline hit.
   */
  private async waitForTunnelUp(port: number, timeoutMs: number): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      if (await this.canConnect(port, 300)) {
        return;
      }
      await new Promise((r) => setTimeout(r, 200));
    }
    throw new Error(
      `iproxy tunnel did not become reachable on 127.0.0.1:${port} within ${timeoutMs}ms. ` +
        `The driver may not be running on the device. Start it with:\n` +
        `  cd native/ios-driver && make serve-device DEVICE_UDID=${this.deviceId}\n` +
        `(or verify device.env has DEVICE_UDID set to the target).`,
    );
  }

  /**
   * One-shot TCP connect attempt to `127.0.0.1:port` with timeout.
   * Resolves `true` when the connect succeeds, `false` on any error
   * or timeout. Pure probe — no data sent, socket destroyed
   * immediately after the probe.
   */
  private canConnect(port: number, timeoutMs: number): Promise<boolean> {
    return new Promise((resolve) => {
      const sock = net.createConnection({ host: DRIVER_HOST, port });
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
   * One-shot throwaway handshake to decide whether the existing
   * listener on `port` is an Atomyx driver. Opens a fresh socket,
   * writes a minimal `ping` request, and waits for `{ok:true,
   * data:{pong:true}}`. Does NOT touch `this.conn` / `this.pending`
   * — this runs before the adapter owns a live connection. Any
   * parse error, timeout, or non-matching response → `false`.
   *
   * This is the escape hatch for a legitimate collision: user
   * already has `make serve-device` running for the same UDID, or a
   * prior `connect()` in the same process left iproxy alive. Without
   * this, `ensurePortFree` would refuse every reselection.
   */
  private probeDriverPing(port: number): Promise<boolean> {
    return new Promise((resolve) => {
      const sock = net.createConnection({ host: DRIVER_HOST, port });
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
        const line = buf.slice(0, nl);
        try {
          const msg = JSON.parse(line) as {
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

  /**
   * Re-establish the TCP connection to the driver after a disconnect.
   * Called internally by `call()` when a request fails with
   * "driver disconnected". Clears stale state so the caller gets a
   * clean session:
   *
   *   - `lastLaunchedBundleId` cleared (the old `XCUIApplication`
   *     reference in the driver process is gone; re-tracking via a
   *     new `launchApp` is the caller's responsibility)
   *   - `pending` waiters all rejected (handled by socket close
   *     listener)
   *
   * Throws if the driver is not reachable within the standard
   * connect timeout — this is a terminal state; the caller must
   * `dispose()` and reconnect via the device router.
   */
  async reconnect(): Promise<void> {
    if (this.conn) {
      this.conn.destroy();
      this.conn = null;
    }
    this.connectionDead = false;
    this.lastLaunchedBundleId = "";
    this.screenSize = null;
    this.buffer = "";
    await this.waitForDriver(this.port);
  }

  private async waitForDriver(port: number): Promise<void> {
    const deadline = Date.now() + CONNECT_TIMEOUT_MS;
    let lastError: Error | null = null;

    while (Date.now() < deadline) {
      try {
        await this.openSocket(port);
        const pong = await this.call("ping", {});
        if (pong.pong === true) return;
      } catch (err) {
        lastError = err as Error;
        await new Promise((r) => setTimeout(r, CONNECT_RETRY_MS));
      }
    }
    throw new Error(
      `iOS driver not reachable on ${DRIVER_HOST}:${port} after ${CONNECT_TIMEOUT_MS}ms. ` +
        `Is \`make serve\` (or the equivalent xcodebuild test-without-building ` +
        `-only-testing:AtomyxDriverUITests/AtomyxDriverUITests/testServeCommands) running? ` +
        `Last error: ${lastError?.message ?? "unknown"}`,
    );
  }

  private openSocket(port: number): Promise<void> {
    return new Promise((resolve, reject) => {
      const sock = net.createConnection({ host: DRIVER_HOST, port });
      const onError = (err: Error) => {
        sock.removeAllListeners();
        sock.destroy();
        reject(err);
      };
      sock.once("error", onError);
      sock.once("connect", () => {
        sock.removeListener("error", onError);
        sock.setEncoding("utf8");
        sock.on("data", (chunk: string) => this.onData(chunk));
        sock.on("error", (err) => this.handleDisconnect(`driver socket error: ${err.message}`));
        sock.on("close", () => this.handleDisconnect("driver disconnected"));
        this.conn = sock;
        this.connectionDead = false;
        resolve();
      });
    });
  }

  /**
   * Shared cleanup for socket close + error events. Marks the
   * connection dead, clears pending waiters with a structured error
   * that includes the failure reason, and clears the tracked
   * `lastLaunchedBundleId` because the driver's `currentApp` reference
   * is now stale (it lived in the dead process).
   */
  private handleDisconnect(reason: string): void {
    if (this.connectionDead) return; // idempotent — close + error may both fire
    this.connectionDead = true;
    this.conn = null;
    this.lastLaunchedBundleId = "";
    this.screenSize = null;
    const err = new Error(reason);
    for (const [, waiter] of this.pending) waiter.reject(err);
    this.pending.clear();
  }

  private onData(chunk: string): void {
    this.buffer += chunk;
    let idx: number;
    while ((idx = this.buffer.indexOf("\n")) >= 0) {
      const line = this.buffer.slice(0, idx);
      this.buffer = this.buffer.slice(idx + 1);
      if (!line.trim()) continue;
      try {
        const msg = JSON.parse(line) as {
          id: number;
          ok: boolean;
          data?: Record<string, unknown>;
          error?: string;
        };
        const waiter = this.pending.get(msg.id);
        if (!waiter) continue;
        this.pending.delete(msg.id);
        if (msg.ok) {
          waiter.resolve(msg.data ?? {});
        } else {
          waiter.reject(new Error(msg.error ?? "driver error"));
        }
      } catch {
        // Malformed line — skip.
      }
    }
  }

  private call(type: string, args: Record<string, unknown>): Promise<Record<string, unknown>> {
    if (!this.conn || this.connectionDead) {
      return Promise.reject(
        new Error(
          "iOS driver not connected. The driver process may have crashed or been terminated. " +
            "Call `reconnect()` on the controller or re-run `make serve` if the underlying " +
            "xcodebuild test process exited.",
        ),
      );
    }
    const id = this.nextId++;
    const payload = JSON.stringify({ id, type, args }) + "\n";
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.conn!.write(payload, (err) => {
        if (err) {
          this.pending.delete(id);
          reject(err);
        }
      });
    });
  }

  // ── Inspector ─────────────────────────────────────────────────────────

  async getUiSummary(): Promise<CompactElement[]> {
    // Driver uses its own tracked `currentApp` reference (set by
    // launchApp). DO NOT pass bundleId here — sending it would force
    // the driver to create a fresh, unlaunched XCUIApplication
    // reference that silently returns 0 elements. The tracked
    // reference is the only one XCUITest can reliably query.
    try {
      const data = await this.call("dumpTree", {});
      const rawElements = (data.elements as DriverElement[]) ?? [];
      return rawElements.map((e) => this.toCompactElement(e));
    } catch (err) {
      // Stale state recovery: if the driver reports no tracked app,
      // our cached bundleId is lying. Clear it so `currentForeground`
      // reflects reality.
      if ((err as Error).message?.includes("no app launched")) {
        this.lastLaunchedBundleId = "";
      }
      throw err;
    }
  }

  /**
   * Map a Swift-side `ElementDescriptor` to the platform-neutral
   * `CompactElement`. Mapping choices:
   *
   *   - `selector.resourceId`  ← `id` (XCUIElement.identifier /
   *                              accessibilityIdentifier)
   *   - `selector.contentDesc` ← `label` (accessibilityLabel), UNLESS it
   *                              equals `id` (dedupe)
   *   - `selector.text`        ← `value` if non-empty
   *   - `clickable`            ← interactive element type whitelist.
   *                              `hittable` is no longer used: the
   *                              Swift driver switched to
   *                              `XCUIApplication.snapshot()` for a
   *                              10–100× perf win, and
   *                              `XCUIElementSnapshot` does not expose
   *                              `isHittable` (it's a live screen-state
   *                              query). Week 1 finding #7 already
   *                              flagged `isHittable` as a poor
   *                              clickable proxy, so no loss in practice.
   *   - `bounds`               ← reconstructed from midpoint + size.
   *                              Swift side reports integer midX/midY/
   *                              width/height in points.
   *   - `isInIme`              ← always false for now. iOS has no global
   *                              keyboard-bounds query in the Week 2
   *                              wire protocol. Revisit in Batch 3 when
   *                              the `getKeyboard` command lands.
   */
  private toCompactElement(e: DriverElement): CompactElement {
    const id = e.id ?? "";
    const label = e.label ?? "";
    const value = e.value ?? "";
    const type = e.type ?? "other";

    const selector: Record<string, string> = {};
    if (id) selector.resourceId = id;
    if (label && label !== id) selector.contentDesc = label;
    if (value) selector.text = value;

    const clickable = INTERACTIVE_ROLES.has(type);

    const w = Math.max(0, Math.round(e.w));
    const h = Math.max(0, Math.round(e.h));
    const cx = Math.round(e.x);
    const cy = Math.round(e.y);

    return {
      selector,
      label: label || value || id,
      role: type,
      clickable,
      enabled: Boolean(e.enabled),
      bounds: {
        left: cx - Math.floor(w / 2),
        top: cy - Math.floor(h / 2),
        right: cx + Math.ceil(w / 2),
        bottom: cy + Math.ceil(h / 2),
      },
      isInIme: false,
    };
  }

  async currentForeground(): Promise<ForegroundInfo> {
    return { appId: this.lastLaunchedBundleId };
  }

  /**
   * Query system keyboard state. Returns `visible: false` with empty
   * keys/bounds when no `UIKeyboard` window is present.
   *
   * Caveat: only detects system keyboards. Custom in-app keyboards
   * (Flutter `GestureDetector` grids, React Native `TouchableOpacity`
   * key views) are regular app views and report `visible: false` even
   * when focused text entry is active via those widgets. Phase 6
   * hardening will add tree-based fallback detection for this case.
   *
   * `packageName` is always `null` on iOS — iOS does not expose the
   * keyboard extension bundle id through XCUITest. Android field
   * preserved for cross-platform port symmetry.
   */
  async getKeyboard(): Promise<KeyboardInfo> {
    const data = await this.call("getKeyboard", {});
    const visible = Boolean(data.visible);
    const bounds = (data.bounds as KeyboardInfo["bounds"]) ?? null;
    const keys = (data.keys as KeyboardInfo["keys"]) ?? [];
    const layout = (data.layout as KeyboardInfo["layout"]) ?? "unknown";
    return {
      visible,
      packageName: null,
      layout,
      bounds,
      keys,
    };
  }

  /**
   * Resolve a selector against the tracked app. Delegates to the
   * Swift `resolveSelector` command which uses snapshot-based
   * strategy chain (resourceId → contentDesc → text → textContains →
   * hint) plus iOS-native `predicate` escape hatch.
   *
   * Returns `{ found: false }` when no strategy matched. The caller
   * (`tap(selector)` / `inputText(selector)`) surfaces this as a
   * structured `ActionResult` failure so the agent can fall back to
   * a different strategy.
   */
  async resolveSelector(selector: Selector): Promise<ResolvedElement> {
    const data = await this.call("resolveSelector", selector as Record<string, unknown>);

    if (!data.found) {
      return { found: false };
    }

    const x = Number(data.x) || 0;
    const y = Number(data.y) || 0;
    const w = Math.max(0, Number(data.w) || 0);
    const h = Math.max(0, Number(data.h) || 0);
    const identifier = (data.identifier as string) ?? "";
    const label = (data.label as string) ?? "";
    const value = (data.value as string) ?? "";
    const obscuredByRaw = data.obscuredBy as
      | { role?: string; identifier?: string; label?: string }
      | undefined;

    return {
      found: true,
      resolvedBy: data.resolvedBy as ResolvedElement["resolvedBy"],
      bounds: {
        left: x - Math.floor(w / 2),
        top: y - Math.floor(h / 2),
        right: x + Math.ceil(w / 2),
        bottom: y + Math.ceil(h / 2),
      },
      resourceId: identifier || null,
      contentDesc: label || null,
      text: value || label || null,
      enabled: Boolean(data.enabled),
      isInIme: false,
      ...(obscuredByRaw
        ? {
            obscuredBy: {
              role: obscuredByRaw.role ?? "",
              identifier: obscuredByRaw.identifier ?? "",
              label: obscuredByRaw.label ?? "",
            },
          }
        : {}),
    };
  }

  async screenshot(): Promise<{ base64: string; format: "png" }> {
    const data = await this.call("screenshot", {});
    return {
      base64: (data.base64 as string) ?? "",
      format: "png",
    };
  }

  // ── Actor ─────────────────────────────────────────────────────────────

  async tapCoordinates(x: number, y: number): Promise<void> {
    await this.call("tapAt", { x, y });
  }

  /**
   * Selector-based tap. Pipeline:
   *
   *   1. `resolveSelector` — find element in snapshot tree
   *   2. `ensureVisible` — scroll-into-view loop until midpoint is
   *      within the viewport (iOS snapshot tree exposes off-screen
   *      elements, coord tap to off-screen point fails silently)
   *   3. Obscurement check — if the resolved element is covered by
   *      another element at its midpoint in z-order, return a
   *      structured error instead of tapping the wrong thing
   *   4. `tapCoordinates` at the current midpoint
   *
   * Returns a structured `ActionResult`:
   *   - `{ok: true, reason: "used: resourceId"}` on success
   *   - `{ok: false, reason: "element not found: ..."}` if unresolved
   *   - `{ok: false, reason: "element obscured by ..."}` if covered
   *   - `{ok: false, reason: "could not scroll into view: ..."}` if
   *     max scroll iterations exhausted or progress stalled
   */
  async tap(selector: Selector): Promise<ActionResult> {
    const prepared = await this.prepareSelectorForAction(selector);
    if (!prepared.ok) return prepared;

    const { cx, cy, resolvedBy } = prepared;
    await this.tapCoordinates(cx, cy);
    return {
      ok: true,
      reason: `tapped element resolved by ${resolvedBy}`,
    };
  }

  /**
   * Selector-based text input. Pipeline:
   *
   *   1. `resolveSelector` + `ensureVisible` + obscurement check
   *      (same as `tap(selector)`)
   *   2. `tapCoordinates` to focus the field
   *   3. 250ms wait for iOS keyboard animation
   *   4. `typeText` via the existing Swift command
   *
   * The focus delay accommodates the keyboard slide-in — typing
   * before the keyboard is ready can be a silent no-op.
   */
  async inputText(selector: Selector, text: string): Promise<ActionResult> {
    const prepared = await this.prepareSelectorForAction(selector);
    if (!prepared.ok) return prepared;

    const { cx, cy, resolvedBy } = prepared;
    await this.tapCoordinates(cx, cy);
    await new Promise((r) => setTimeout(r, 250));
    await this.call("typeText", { text });
    return {
      ok: true,
      reason: `typed ${text.length} chars into element resolved by ${resolvedBy}`,
    };
  }

  /**
   * Shared pipeline for `tap(selector)` and `inputText(selector)`:
   * resolve → ensure-visible (with scroll loop) → obscurement check.
   * Returns the computed tap midpoint on success, or a structured
   * failure result mirroring `ActionResult`.
   */
  private async prepareSelectorForAction(
    selector: Selector,
  ): Promise<
    | { ok: true; cx: number; cy: number; resolvedBy: string | undefined }
    | { ok: false; reason: string }
  > {
    let resolved: ResolvedElement;
    try {
      resolved = await this.ensureVisible(selector);
    } catch (err) {
      return {
        ok: false,
        reason: `could not scroll element into view: ${(err as Error).message}`,
      };
    }

    if (!resolved.found || !resolved.bounds) {
      return {
        ok: false,
        reason: `element not found for selector ${JSON.stringify(selector)}`,
      };
    }

    if (resolved.obscuredBy) {
      const o = resolved.obscuredBy;
      return {
        ok: false,
        reason:
          `element is visually obscured by [role=${o.role} ` +
          `identifier="${o.identifier}" label="${o.label}"]. ` +
          `Dismiss the obscuring element (modal, sheet, alert) before retrying, ` +
          `OR use find_element on the obscurer to tap it directly.`,
      };
    }

    return {
      ok: true,
      cx: (resolved.bounds.left + resolved.bounds.right) / 2,
      cy: (resolved.bounds.top + resolved.bounds.bottom) / 2,
      resolvedBy: resolved.resolvedBy,
    };
  }

  /**
   * Lazy-fetched, session-cached screen size. First call makes one
   * RPC to the Swift `getScreenSize` command; subsequent calls read
   * from the cache until it is invalidated (launchApp,
   * forceStopApp, reconnect, disconnect).
   */
  private async getScreenSize(): Promise<{ width: number; height: number }> {
    if (this.screenSize) return this.screenSize;
    const data = await this.call("getScreenSize", {});
    this.screenSize = {
      width: Number(data.width) || 0,
      height: Number(data.height) || 0,
    };
    return this.screenSize;
  }

  /**
   * Resolve the selector, and if the resolved midpoint is outside
   * the viewport, scroll to bring it in. Re-resolves after each
   * scroll because bounds shift. Max 8 iterations with adaptive
   * scroll distance capped at 60% of screen height per swipe.
   *
   * Progress check: if post-scroll bounds match pre-scroll bounds
   * exactly, the scroll container didn't move (end of list or
   * wrong container under the swipe column). Abort with an
   * actionable error instead of spinning to the iteration cap.
   *
   * Swipe column: element's own midX, clamped to a 40pt margin
   * from screen edges. This ensures we scroll the container that
   * actually holds the element (critical for nested scroll views)
   * rather than defaulting to screen center.
   *
   * Does NOT scroll if the initial resolve shows the element is
   * already in-viewport — the first loop iteration exits early.
   * Does NOT retry on obscurement; that's surfaced as-is through
   * the returned ResolvedElement.
   */
  private async ensureVisible(selector: Selector): Promise<ResolvedElement> {
    const MAX_ITERATIONS = 8;
    const EDGE_MARGIN = 40;
    const MAX_SCROLL_FRACTION = 0.6;
    const SCROLL_DURATION_MS = 200;
    const ANIMATION_WAIT_MS = 500;

    const screen = await this.getScreenSize();

    let resolved = await this.resolveSelector(selector);

    // Phase 0: scroll-search for virtualized lists. iOS
    // UITableView/UICollectionView recycles off-screen cells, so the
    // target may exist logically but be absent from the current
    // snapshot. Probe the scrollable area in both directions until
    // the element materializes. This is the fix for "list scrolled
    // past the target in either direction" — the most common reason
    // `resolveSelector` returns not-found on selectors the agent
    // knows exist (e.g. after going Back from a deep screen, the
    // root list stayed scrolled to the bottom).
    //
    // Direction order: UP first (toward list start, where most
    // anchor items live — primary account row, first section). If
    // not found after the up budget, switch to DOWN to cover the
    // case where the list started at the top and the target is
    // further down. Budget is intentionally small — this is a
    // fallback, not the primary scroll path; `tap_and_wait_transition`
    // + `get_ui_tree` remain the canonical "orient before acting"
    // workflow.
    if (!resolved.found) {
      resolved = await this.scrollSearchForSelector(selector, screen);
    }

    if (!resolved.found || !resolved.bounds) return resolved;

    let previousBounds: ResolvedElement["bounds"] = null as unknown as ResolvedElement["bounds"];

    for (let iteration = 0; iteration < MAX_ITERATIONS; iteration++) {
      const bounds = resolved.bounds!;
      if (this.isMidpointInViewport(bounds, screen)) {
        return resolved;
      }

      // Progress check: bounds identical to previous iteration means
      // the swipe didn't scroll the target container.
      if (previousBounds && boundsEqual(previousBounds, bounds)) {
        throw new Error(
          `element at ${describeBounds(bounds)} did not move after scroll ` +
            `iteration ${iteration}. Likely end of scrollable region or ` +
            `scroll container not under swipe column ` +
            `(x=${Math.round((bounds.left + bounds.right) / 2)}). Screen: ` +
            `${screen.width}x${screen.height}.`,
        );
      }
      previousBounds = bounds;

      // Scroll computation: vertical swipe at the element's X column,
      // distance capped at MAX_SCROLL_FRACTION of screen height.
      const elemMidX = (bounds.left + bounds.right) / 2;
      const elemMidY = (bounds.top + bounds.bottom) / 2;
      const viewportMidY = screen.height / 2;

      const swipeX = clamp(elemMidX, EDGE_MARGIN, screen.width - EDGE_MARGIN);
      const deltaY = elemMidY - viewportMidY;
      const scrollAmount = Math.min(
        Math.abs(deltaY),
        screen.height * MAX_SCROLL_FRACTION,
      );
      // Element below viewport → finger drags bottom-to-top (content moves up)
      // Element above viewport → finger drags top-to-bottom (content moves down)
      const elementBelow = deltaY > 0;
      const fromY = elementBelow
        ? viewportMidY + scrollAmount / 2
        : viewportMidY - scrollAmount / 2;
      const toY = elementBelow
        ? viewportMidY - scrollAmount / 2
        : viewportMidY + scrollAmount / 2;

      await this.swipe(swipeX, fromY, swipeX, toY, SCROLL_DURATION_MS);
      await new Promise((r) => setTimeout(r, ANIMATION_WAIT_MS));

      // Re-resolve; element may have moved or the container may have
      // recycled its nodes (table view cell reuse).
      resolved = await this.resolveSelector(selector);
      if (!resolved.found || !resolved.bounds) {
        throw new Error(
          `element lost after scroll attempt ${iteration + 1} — ` +
            `resolveSelector no longer matches. Container may have ` +
            `reloaded or the agent's selector was ambiguous.`,
        );
      }
    }

    // Max iterations exhausted.
    throw new Error(
      `element remained off-screen after ${MAX_ITERATIONS} scroll iterations. ` +
        `Final bounds: ${describeBounds(resolved.bounds!)}. ` +
        `Screen: ${screen.width}x${screen.height}. Consider: element inside ` +
        `non-scrolling container, nested scroll view not reached by the ` +
        `swipe column, or the list is longer than the adapter's scroll budget.`,
    );
  }

  /**
   * Safe-area aware viewport check.
   *
   * `app.frame` on iOS returns the full screen rect (e.g. 430x932 on
   * iPhone 12 Pro Max) — status bar, notch, and home indicator
   * included. An element whose midpoint lands in those edge zones is
   * technically "on screen" by a naive bounds check but is NOT
   * reliably tappable: the home indicator captures bottom swipes, the
   * status bar absorbs top taps, and cells partially under either
   * clip visually.
   *
   * We inset the viewport by conservative margins so `ensureVisible`
   * keeps scrolling until the element is comfortably inside the
   * interactive area. Costs at most 1 extra scroll iteration for
   * legitimate edge items — ensureVisible re-resolves per iteration
   * so the loop self-terminates once the element settles.
   *
   * Margins chosen from modern iPhone chrome:
   *   top    = 60pt — status bar (44pt) + buffer for NavigationBar items
   *   bottom = 50pt — home indicator (34pt) + buffer for TabBar items
   *   horiz  = 0    — horizontal scroll is rare; don't over-inset
   */
  private isMidpointInViewport(
    bounds: { left: number; top: number; right: number; bottom: number },
    screen: { width: number; height: number },
  ): boolean {
    const TOP_INSET = 60;
    const BOTTOM_INSET = 50;
    const cx = (bounds.left + bounds.right) / 2;
    const cy = (bounds.top + bounds.bottom) / 2;
    return (
      cx >= 0 &&
      cx < screen.width &&
      cy >= TOP_INSET &&
      cy < screen.height - BOTTOM_INSET
    );
  }

  /**
   * Probe the current screen for a selector that resolveSelector
   * failed to find, under the assumption that the target sits in a
   * virtualized scroll view and the current scroll position recycled
   * it off-screen. Swipes the viewport center column in alternating
   * directions (up budget first, then down budget) calling
   * resolveSelector between each swipe. First hit wins.
   *
   * Returns whatever resolveSelector returns — caller handles both
   * the found + bounds path (hands off to the positional scroll
   * loop) and the still-not-found path (hard fail from
   * prepareSelectorForAction with the standard "element not found"
   * message).
   *
   * Safety characteristics:
   *   - Budget-bounded: at most UP_BUDGET + DOWN_BUDGET swipes.
   *     Each swipe is ~60% of screen height, so the full budget
   *     covers roughly (UP + DOWN) * 0.6 screens of content —
   *     enough for typical Settings-style lists without being so
   *     aggressive it overruns uncommon nested scrollers.
   *   - Read-only with respect to app state aside from scroll
   *     position: no taps, no text input, no key presses. Scroll
   *     is the minimum mutation needed to materialize a recycled
   *     cell.
   *   - Idempotent termination: stops immediately on first find.
   *     Caller's positional loop then handles fine-grained
   *     centering via `isMidpointInViewport`.
   */
  private async scrollSearchForSelector(
    selector: Selector,
    screen: { width: number; height: number },
  ): Promise<ResolvedElement> {
    const UP_BUDGET = 6;
    const DOWN_BUDGET = 6;
    const SCROLL_FRACTION = 0.6;
    const SCROLL_DURATION_MS = 200;
    const ANIMATION_WAIT_MS = 400;

    const centerX = screen.width / 2;
    const viewportMidY = screen.height / 2;
    const delta = (screen.height * SCROLL_FRACTION) / 2;

    const doSwipe = async (direction: "up" | "down"): Promise<void> => {
      // "up" = reveal items logically ABOVE current viewport:
      // finger drags top → bottom, content moves DOWN.
      // "down" = reveal items BELOW: finger drags bottom → top,
      // content moves UP.
      const fromY = direction === "up" ? viewportMidY - delta : viewportMidY + delta;
      const toY = direction === "up" ? viewportMidY + delta : viewportMidY - delta;
      await this.swipe(centerX, fromY, centerX, toY, SCROLL_DURATION_MS);
      await new Promise((r) => setTimeout(r, ANIMATION_WAIT_MS));
    };

    for (let i = 0; i < UP_BUDGET; i++) {
      await doSwipe("up");
      const r = await this.resolveSelector(selector);
      if (r.found) return r;
    }
    for (let i = 0; i < DOWN_BUDGET; i++) {
      await doSwipe("down");
      const r = await this.resolveSelector(selector);
      if (r.found) return r;
    }
    return { found: false };
  }

  /**
   * Clear the currently-focused input by sending delete keys. iOS has
   * no direct "clear" primitive — the adapter forwards to the Swift
   * `clearFocusedInput` command which types `XCUIKeyboardKey.delete`
   * in bulk. See `ClearFocusedInputCommand` docstring for the 500-key
   * cap rationale.
   */
  async clearFocusedInput(): Promise<ActionResult> {
    const data = await this.call("clearFocusedInput", {});
    return {
      ok: true,
      reason: `sent ${data.deleted} delete keys`,
    };
  }

  async longPressCoordinates(x: number, y: number, durationMs?: number): Promise<void> {
    const args: Record<string, unknown> = { x, y };
    if (durationMs !== undefined) args.durationMs = durationMs;
    await this.call("longPressAt", args);
  }

  async swipe(
    fromX: number,
    fromY: number,
    toX: number,
    toY: number,
    durationMs?: number,
  ): Promise<void> {
    const args: Record<string, unknown> = { fromX, fromY, toX, toY };
    if (durationMs !== undefined) args.durationMs = durationMs;
    await this.call("swipe", args);
  }

  async pressKey(key: "back" | "home" | "enter"): Promise<ActionResult> {
    const data = await this.call("pressKey", { key });
    const affordanceFound = Boolean(data.affordanceFound);
    const strategy = (data.strategy as string) ?? "unknown";

    if (affordanceFound) {
      return { ok: true, reason: `used: ${strategy}` };
    }
    // Unverifiable path (only "back" edge swipe today). Return a
    // structured failure so the agent can fall back to find_element
    // on screen-specific back affordances (Cancel, Done, Close, X).
    return {
      ok: false,
      reason:
        `iOS pressKey("${key}") attempted ${strategy} but no verifiable affordance was used. ` +
        `Fall back to find_element(label IN {"Back","Cancel","Done","Close"}) + tap for screen-specific back.`,
    };
  }

  /**
   * Type text into the currently-focused input. iOS handles this via
   * one `XCUIApplication.typeText()` call — the system keyboard accepts
   * the whole string and dispatches per-character events internally.
   *
   * `perKeyDelayMs` is ignored on iOS (no native control — typeText
   * dispatches at XCUITest's internal speed). `clearFirst` is honored
   * via the `clearFocusedInput` command: when true, the adapter
   * composes clearFocusedInput + typeText. The caller must still
   * ensure the target field is focused (tap it first).
   */
  async typeViaKeyboard(
    text: string,
    _perKeyDelayMs?: number,
    clearFirst?: boolean,
  ): Promise<TypeKeyboardResult> {
    if (clearFirst) {
      await this.call("clearFocusedInput", {});
    }
    const data = await this.call("typeText", { text });
    return {
      success: Boolean(data.success),
      typed: Number(data.typed) || 0,
      total: Number(data.total) || 0,
      reason: (data.reason as string) ?? "ok",
    };
  }

  // ── AppManager ────────────────────────────────────────────────────────

  async launchApp(appId: string): Promise<void> {
    await this.call("launchApp", { bundleId: appId });
    this.lastLaunchedBundleId = appId;
    // New app may have a different viewport (iPad Split View,
    // different safe area). Force a refetch on next ensureVisible.
    this.screenSize = null;
  }

  async forceStopApp(appId: string): Promise<void> {
    await this.call("forceStopApp", { bundleId: appId });
    if (this.lastLaunchedBundleId === appId) {
      this.lastLaunchedBundleId = "";
      this.screenSize = null;
    }
  }

  /**
   * List installed apps. Branches on device kind because the sim and
   * physical-device toolchains have completely different enumeration
   * paths:
   *
   *   - Simulator: `xcrun simctl listapps <UDID>` — returns an
   *     old-style NeXTSTEP plist (dict keyed by bundle id). Needs
   *     `plutil -convert json` to parse.
   *   - Physical device: `xcrun devicectl device info apps
   *     --device <UDID> --json-output -` — returns clean JSON. Part
   *     of Xcode 15+ toolchain; no extra libimobiledevice dependency
   *     beyond what iproxy already needs.
   *
   * Both are host-side shell-outs rather than Swift driver commands
   * because the Xcode toolchain has direct access and the driver
   * would just proxy the same call.
   */
  async listApps(): Promise<InstalledApp[]> {
    if (this.kind === "device") {
      return this.listAppsOnDevice();
    }
    return this.listAppsOnSimulator();
  }

  private async listAppsOnSimulator(): Promise<InstalledApp[]> {
    const { stdout } = await execFileAsync("xcrun", ["simctl", "listapps", this.deviceId]);
    const json = await this.pipePlutilToJson(stdout);
    const parsed = JSON.parse(json) as Record<string, Record<string, unknown>>;
    const apps: InstalledApp[] = [];
    for (const [bundleId, info] of Object.entries(parsed)) {
      const label =
        (info.CFBundleDisplayName as string | undefined) ??
        (info.CFBundleName as string | undefined);
      apps.push({ appId: bundleId, ...(label ? { label } : {}) });
    }
    return apps.sort((a, b) => a.appId.localeCompare(b.appId));
  }

  /**
   * `devicectl` JSON shape (Xcode 15+):
   *
   *     {
   *       "info": { ... },
   *       "result": {
   *         "apps": [
   *           {
   *             "bundleIdentifier": "com.example.app",
   *             "name": "Example",
   *             "bundleVersion": "1.0",
   *             ...
   *           },
   *           ...
   *         ]
   *       }
   *     }
   *
   * We only care about `bundleIdentifier` + `name`. Other fields
   * (version, installationURL, appClip, builtByDeveloper) are
   * ignored because the port's `InstalledApp` shape doesn't carry
   * them — adding them would be platform-leak.
   */
  private async listAppsOnDevice(): Promise<InstalledApp[]> {
    const { stdout } = await execFileAsync("xcrun", [
      "devicectl",
      "device",
      "info",
      "apps",
      "--device",
      this.deviceId,
      "--json-output",
      "-",
    ]);
    const parsed = JSON.parse(stdout) as {
      result?: {
        apps?: Array<{ bundleIdentifier: string; name?: string }>;
      };
    };
    const apps = parsed.result?.apps ?? [];
    return apps
      .map((a) => ({
        appId: a.bundleIdentifier,
        ...(a.name ? { label: a.name } : {}),
      }))
      .filter((a) => a.appId)
      .sort((a, b) => a.appId.localeCompare(b.appId));
  }

  private pipePlutilToJson(rawPlist: string): Promise<string> {
    return new Promise((resolve, reject) => {
      const proc = spawn("plutil", ["-convert", "json", "-r", "-o", "-", "--", "-"]);
      let stdout = "";
      let stderr = "";
      proc.stdout.setEncoding("utf8");
      proc.stderr.setEncoding("utf8");
      proc.stdout.on("data", (c: string) => (stdout += c));
      proc.stderr.on("data", (c: string) => (stderr += c));
      proc.on("error", reject);
      proc.on("close", (code) => {
        if (code === 0) resolve(stdout);
        else reject(new Error(`plutil exited ${code}: ${stderr}`));
      });
      proc.stdin.write(rawPlist);
      proc.stdin.end();
    });
  }

  // ── Lifecycle ─────────────────────────────────────────────────────────

  async dispose(): Promise<void> {
    if (this.conn) {
      this.conn.end();
      this.conn = null;
    }
    if (this.iproxyProc) {
      this.iproxyProc.kill("SIGTERM");
      this.iproxyProc = null;
    }
  }

  // ── Phase 3 scope (unimplemented) ─────────────────────────────────────

  private nope(method: string): never {
    throw new Error(
      `ios.${method} not implemented in Week 2 baseline. Scheduled for Phase 3 — see docs/ios.md.`,
    );
  }

  /**
   * Dump the hierarchical accessibility tree. Consumed by tool-layer
   * strategies that walk parent-child relationships — notably
   * `StructuralInputFinder`'s 4-strategy chain which looks for
   * editable text fields by their semantic label via preceding-sibling
   * and container-descendant patterns.
   *
   * Maps the Swift `dumpRawTree` wire response to the port's
   * `RawElement` shape. Key mapping decisions:
   *
   *   - `elementType` string (e.g. "textField") → `className`, so
   *     `find-input.ts#isEditText` matches via substring ("textfield")
   *   - `identifier` → `resourceId` (same pattern as CompactElement)
   *   - `label` → `contentDesc`
   *   - `value` (if present) → `text`
   *   - iOS snapshot has no `clickable` bit — derive from interactive
   *     elementType whitelist, same as `toCompactElement`
   *
   * `elementId` falls back to a synthetic per-node counter because
   * iOS nodes don't have a stable unique id outside of `identifier`
   * (which is only set on elements developers annotated). Counter
   * resets each call — not stable across dumps, but consumers
   * (find-input tree walkers) only need it for within-walk identity.
   */
  async getUiTree(): Promise<RawElement> {
    const data = await this.call("dumpRawTree", {});
    const root = (data.root as Record<string, unknown>) ?? {};
    const counter = { n: 0 };
    return this.toRawElement(root, counter);
  }

  private toRawElement(node: Record<string, unknown>, counter: { n: number }): RawElement {
    counter.n += 1;
    const elementType = (node.elementType as string) ?? "other";
    const identifier = (node.identifier as string) ?? "";
    const label = (node.label as string) ?? "";
    const value = (node.value as string) ?? "";
    const bounds = node.bounds as RawElement["bounds"];
    const children = node.children as Array<Record<string, unknown>> | undefined;

    const el: RawElement = {
      elementId: identifier || `ios-${counter.n}`,
      className: elementType,
      resourceId: identifier || undefined,
      contentDesc: label || undefined,
      text: value || undefined,
      bounds,
      enabled: Boolean(node.enabled),
      clickable: INTERACTIVE_ROLES.has(elementType),
    };
    if (Array.isArray(children) && children.length > 0) {
      el.children = children.map((c) => this.toRawElement(c, counter));
    }
    return el;
  }
}
