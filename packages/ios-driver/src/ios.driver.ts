import { exec } from "node:child_process";
import { promisify } from "node:util";
import type {
  CallOptions,
  Capabilities,
  DeviceInfo,
  Driver,
  ForegroundInfo,
  Gesture,
  InstalledApp,
  KeyCode,
  KeyResult,
  LaunchArgs,
  Point,
  Size,
  TreeNode,
} from "@atomyx/driver";
import { TcpClient } from "./tcp-client.js";
import { Iproxy } from "./iproxy.js";
import { XctestLauncher } from "./xctest-launcher.js";
import { normalizeIosTree, type IosRawElement } from "./tree-normalizer.js";

const execAsync = promisify(exec);

/**
 * Atomyx iOS Driver — implements the `Driver` port from
 * `@atomyx/core` over the Swift XCUITest runner's TCP+JSON
 * protocol.
 *
 * Transport topology:
 *
 *   Simulator:
 *     host TS → 127.0.0.1:22087 → XCUITest process (shared netns)
 *
 *   Physical device:
 *     host TS → 127.0.0.1:22087 → iproxy (USB usbmux) →
 *     XCUITest process → 127.0.0.1:22087 on the device itself
 *
 * `kind === "simulator"` means loopback is direct; `kind ===
 * "device"` means we spawn an `iproxy` tunnel first. Everything
 * else (the TCP client, the tree normalizer, the primitive
 * dispatch) is identical across the two.
 *
 * What this driver deliberately does NOT do:
 *
 *   - Selector resolution. The driver speaks ONLY coordinates;
 *     all selector logic lives in `@atomyx/driver` via
 *     `compileSelector` + `Finder` operating on the canonical
 *     `TreeNode` this adapter emits from `dumpRawTree`.
 *
 *   - Scroll-into-view. Core's `ScrollController` drives this
 *     host-side by composing `hierarchy()` + `swipe()`
 *     primitives.
 *
 *   - Obscurement detection. Core's `detectObscurement` runs on
 *     the canonical `TreeNode` this driver emits.
 *
 *   - Ambiguity reporting / priority broadening / selector
 *     dedupe. Core.
 *
 * That leaves the driver as: (1) transport, (2) iproxy
 * lifecycle, (3) tree normalization, (4) 1:1 mapping from
 * `Driver` interface methods to Swift commands.
 */

export interface IosDriverOptions {
  /**
   * "simulator" shares host network with loopback — no iproxy.
   * "device" spawns `iproxy ${port}:${port} -u ${udid}`.
   */
  readonly kind: "simulator" | "device";
  /**
   * Device UDID. Required for `kind: "device"` (passed to iproxy);
   * informational only for `kind: "simulator"` — simctl commands
   * that need a UDID should resolve one themselves.
   */
  readonly udid: string;
  /**
   * Host-side TCP port. Defaults to 22087 (the Swift driver's
   * hardcoded bind port). Change when multiple drivers run
   * concurrently — each needs a distinct host port.
   */
  readonly port?: number;
  /** TCP client connect retry budget. Default 30s. */
  readonly connectTimeoutMs?: number;
  /** Per-call request timeout. Default 15s. */
  readonly requestTimeoutMs?: number;

  /**
   * When true, `connect()` will auto-detect and spawn the
   * XCUITest runner if it is not already listening. Requires the
   * native iOS driver project to be present on disk (either in
   * the repo tree or via ATOMYX_IOS_DRIVER_DIR env var).
   *
   * Default: false (preserve backward compat with manual
   * `make serve`).
   */
  readonly autoLaunch?: boolean;
  /** Path to `native/ios-driver/` directory. Used when autoLaunch is true. */
  readonly projectDir?: string;
  /** Apple Development Team ID. Required for autoLaunch on real devices. */
  readonly devTeam?: string;
}

export class IosDriver implements Driver {
  readonly platform = "ios" as const;

  /**
   * Capabilities populated at `connect()` time from the Swift
   * runner's ping response. Gesture capabilities
   * (`canMultiPointer`, `canPressure`) reflect what the runner's
   * active backend can dispatch — `true` when the private-XCTest
   * event-record backend is live, `false` on the public-API
   * coordinate fallback. Everything else is a static property of
   * the driver version.
   */
  private _capabilities: Capabilities = {
    canScreenshot: true,
    canEraseText: true, // Swift driver exposes clearFocusedInput
    canWaitForIdle: false, // no native idle primitive yet — core polls
    canSetLocation: false,
    canSetOrientation: false,
    canHideKeyboard: true, // Swift driver exposes hideKeyboard
    canMultiPointer: false,
    canPressure: false,
    supportedKeyCodes: ["back", "home", "enter"],
  };

  get capabilities(): Capabilities {
    return this._capabilities;
  }

  /**
   * Internal mechanism the runner uses to dispatch gestures
   * (`"event-record"` or `"coordinate"`). Captured from the ping
   * response for log correlation only. Callers MUST NOT branch
   * on this — branch on `capabilities` instead. `null` until
   * `connect()` runs.
   */
  private _gestureMechanism: "event-record" | "coordinate" | null = null;

  get gestureMechanism(): "event-record" | "coordinate" | null {
    return this._gestureMechanism;
  }

  private readonly port: number;
  private readonly tcp: TcpClient;
  private readonly iproxy: Iproxy | null;
  private readonly launcher: XctestLauncher | null;
  private lastLaunchedBundleId = "";

  constructor(private readonly opts: IosDriverOptions) {
    this.port = opts.port ?? 22087;
    this.tcp = new TcpClient({
      host: "127.0.0.1",
      port: this.port,
      connectTimeoutMs: opts.connectTimeoutMs ?? 30_000,
      requestTimeoutMs: opts.requestTimeoutMs ?? 15_000,
    });
    this.iproxy =
      opts.kind === "device"
        ? new Iproxy({
            udid: opts.udid,
            hostPort: this.port,
            devicePort: this.port,
          })
        : null;
    this.launcher =
      opts.autoLaunch
        ? new XctestLauncher({
            udid: opts.udid,
            kind: opts.kind,
            port: this.port,
            projectDir: opts.projectDir,
            devTeam: opts.devTeam ?? process.env.ATOMYX_DEV_TEAM,
            log: (msg) =>
              process.stderr.write(`[atomyx/ios] ${msg}\n`),
          })
        : null;
  }

  // ── Lifecycle ────────────────────────────────────────────────

  async connect(opts?: CallOptions): Promise<void> {
    // Auto-launch: detect-or-spawn the XCUITest runner before
    // opening the transport. Must happen before iproxy — the
    // driver process must be running before the tunnel has
    // anything to connect to.
    if (this.launcher) {
      await this.launcher.ensureRunning();
    }
    if (this.iproxy) {
      await this.iproxy.start();
    }
    await this.tcp.connect();
    // Ping handshake verifies the Swift driver is up and speaking
    // our protocol — not just "a port is listening". Also
    // populates gesture capabilities (`canMultiPointer`,
    // `canPressure`) and the active synthesizer name so the
    // YAML validator can reject unsupported gesture shapes
    // before they leave the host.
    const pong = await this.tcp.call("ping", {}, { signal: opts?.signal });
    this.applyPingCapabilities(pong);
  }

  private applyPingCapabilities(pong: Record<string, unknown>): void {
    const caps = pong["capabilities"] as
      | { canMultiPointer?: unknown; canPressure?: unknown }
      | undefined;
    if (caps) {
      this._capabilities = {
        ...this._capabilities,
        canMultiPointer: caps.canMultiPointer === true,
        canPressure: caps.canPressure === true,
      };
    }
    const mechanism = pong["mechanism"];
    if (mechanism === "event-record" || mechanism === "coordinate") {
      this._gestureMechanism = mechanism;
    } else {
      this._gestureMechanism = null;
    }
  }

  async disconnect(): Promise<void> {
    await this.tcp.disconnect();
    if (this.iproxy) {
      await this.iproxy.stop();
    }
    // Only shut down the launcher if WE spawned the child.
    // If we detected a pre-existing runner, shutdown() is a no-op.
    if (this.launcher) {
      await this.launcher.shutdown();
    }
    this.lastLaunchedBundleId = "";
  }

  isConnected(): boolean {
    return this.tcp.isConnected();
  }

  /**
   * Reconnect after a disconnect (XCUITest crash, socket timeout,
   * etc.). Re-opens the TCP transport and verifies the connection
   * is live via a ping handshake before returning.
   *
   * If iproxy was active, it restarts the tunnel first. If
   * autoLaunch was set, the launcher re-checks/re-spawns the
   * XCUITest runner.
   *
   * Throws if the ping handshake fails — the caller gets an
   * actionable error rather than a silently stale connection.
   */
  async reconnect(): Promise<void> {
    if (this.launcher) {
      await this.launcher.ensureRunning();
    }
    if (this.iproxy) {
      await this.iproxy.stop();
      await this.iproxy.start();
    }
    await this.tcp.reconnect();
    // Stale binding detection: verify the Swift driver actually
    // responds. Without this, a reconnect to a port that happens
    // to be listening (sim driver on same port, stale tunnel) would
    // silently succeed but dispatch commands to the wrong target.
    // Also re-reads capabilities so capability drift across
    // reconnects (e.g. a sim switch between private-capable and
    // public-only builds) is picked up.
    try {
      const pong = await this.tcp.call("ping", {});
      this.applyPingCapabilities(pong);
    } catch (err) {
      throw new Error(
        `iOS driver reconnect succeeded at transport level but ping ` +
          `handshake failed — the XCUITest runner may have crashed or ` +
          `the tunnel is routing to a stale listener. Try: kill any ` +
          `existing driver processes and reconnect. ` +
          `(${(err as Error).message})`,
      );
    }
    this.lastLaunchedBundleId = "";
  }

  // ── Hierarchy ────────────────────────────────────────────────

  async hierarchy(opts?: CallOptions): Promise<TreeNode> {
    // If the runner has no app bound (operator launched the target
    // app from outside Studio), hand it a shortlist of currently-
    // running UIKit bundle ids. The runner picks whichever is in
    // foreground via `XCUIApplication.state` and snapshots it.
    // This avoids forcing an explicit `launchApp` round-trip when
    // the user is just inspecting a foreground app on the
    // simulator. Best-effort; query failures pass an empty list
    // and the runner falls back to its existing error path.
    const args: Record<string, unknown> = {};
    if (!this.lastLaunchedBundleId && this.opts.kind === "simulator") {
      const candidates = await this.listRunningUiKitBundleIds().catch(() => []);
      if (candidates.length > 0) {
        args["bundleIdCandidates"] = candidates;
      }
    }
    const data = await this.tcp.call("dumpRawTree", args, { signal: opts?.signal });
    if (typeof data.bundleId === "string" && data.bundleId.length > 0) {
      this.lastLaunchedBundleId = data.bundleId;
    }
    const root = (data.root as IosRawElement | undefined) ?? {
      elementType: "other",
    };
    const wire = normalizeIosTree(root);
    return wire as unknown as TreeNode;
  }

  /// Query the simulator for currently-running UIKit application
  /// bundle ids. Output of `simctl spawn <udid> launchctl list`
  /// includes one row per launched job; UIKit apps are exposed as
  /// labels of the form `UIKitApplication:<bundleId>[<pid>][...`.
  /// We extract the `<bundleId>` slice. On failure or if no apps
  /// are running, returns an empty array.
  private async listRunningUiKitBundleIds(): Promise<string[]> {
    const { stdout } = await execAsync(
      `xcrun simctl spawn ${this.opts.udid} launchctl list`,
      { timeout: 5_000 },
    );
    const ids = new Set<string>();
    for (const line of stdout.split("\n")) {
      const m = /UIKitApplication:([^\s\[]+)/.exec(line);
      if (m && m[1]) ids.add(m[1]);
    }
    return [...ids];
  }

  async waitForIdle(_timeoutMs: number, _opts?: CallOptions): Promise<boolean> {
    // capabilities.canWaitForIdle === false; core should not
    // invoke this. Defensive no-op.
    return true;
  }

  // ── Gesture primitives ──────────────────────────────────────

  async tap(point: Point, opts?: CallOptions): Promise<void> {
    await this.tcp.call("tapAt", { x: point.x, y: point.y }, { signal: opts?.signal });
  }

  async longPress(point: Point, durationMs: number, opts?: CallOptions): Promise<void> {
    await this.tcp.call(
      "longPressAt",
      { x: point.x, y: point.y, durationMs },
      { signal: opts?.signal },
    );
  }

  async dispatchGesture(gesture: Gesture, opts?: CallOptions): Promise<void> {
    if (gesture.pointers.length > 1 && !this._capabilities.canMultiPointer) {
      throw new Error(
        `iOS driver cannot dispatch ${gesture.pointers.length}-pointer gesture: ` +
          `multi-pointer capability is unavailable on this Xcode / iOS combination. ` +
          `The runner falls back to single-pointer dispatch when the multi-pointer ` +
          `runtime symbols are missing — typically resolved by upgrading Xcode.`,
      );
    }
    const pointers = gesture.pointers.map((p) => ({
      id: p.id,
      waypoints: p.waypoints.map((w) => ({
        phase: w.phase,
        x: w.point.x,
        y: w.point.y,
        atOffsetSeconds: w.atOffsetSeconds,
        ...(w.pressure !== undefined ? { pressure: w.pressure } : {}),
      })),
    }));
    await this.tcp.call("dispatchPointer", { pointers }, { signal: opts?.signal });
  }

  async swipe(
    from: Point,
    to: Point,
    durationMs: number,
    opts?: CallOptions,
  ): Promise<void> {
    await this.tcp.call(
      "swipe",
      {
        fromX: from.x,
        fromY: from.y,
        toX: to.x,
        toY: to.y,
        durationMs,
      },
      { signal: opts?.signal },
    );
  }

  // ── Text input ──────────────────────────────────────────────

  async inputText(text: string, opts?: CallOptions): Promise<void> {
    // Uses iOS native `XCUIApplication.typeText()` under the hood
    // via the Swift `typeText` command. Requires a focused field.
    await this.tcp.call("typeText", { text }, { signal: opts?.signal });
  }

  async eraseText(count: number, opts?: CallOptions): Promise<void> {
    // Swift driver has `clearFocusedInput` which bulk-sends
    // delete keys to the focused field. The arg name on the
    // wire is `maxDeletes` — Swift caps it at 500 to avoid
    // runaway repeat counts.
    await this.tcp.call(
      "clearFocusedInput",
      { maxDeletes: count },
      { signal: opts?.signal },
    );
  }

  async pressKey(key: KeyCode, opts?: CallOptions): Promise<KeyResult> {
    const data = await this.tcp.call("pressKey", { key }, { signal: opts?.signal });
    // The Swift runner returns {key, affordanceFound, strategy}.
    // affordanceFound=true means a verifiable control was used
    // (nav_bar_back, home device press, enter into focused field);
    // false means a best-effort gesture was dispatched but the
    // driver cannot confirm any effect (e.g. edge_swipe_best_effort
    // for iOS back gesture). We surface that directly as the
    // KeyResult ok flag, and pass the strategy name through as
    // reason for agent observability.
    const affordanceFound = data.affordanceFound === true;
    const strategy = typeof data.strategy === "string" ? data.strategy : undefined;
    return {
      ok: affordanceFound,
      reason: strategy,
    };
  }

  async hideKeyboard(opts?: CallOptions): Promise<KeyResult> {
    const data = await this.tcp.call("hideKeyboard", {}, { signal: opts?.signal });
    const ok = data.ok === true;
    const strategy =
      typeof data.strategy === "string" ? data.strategy : undefined;
    return { ok, reason: strategy };
  }

  // ── App lifecycle ────────────────────────────────────────────

  async launchApp(
    bundleId: string,
    args?: LaunchArgs,
    opts?: CallOptions,
  ): Promise<void> {
    const noReset = args?.noReset === true;
    if (noReset && this.lastLaunchedBundleId === bundleId) {
      return;
    }
    await this.tcp.call("launchApp", { bundleId, noReset }, { signal: opts?.signal });
    this.lastLaunchedBundleId = bundleId;
  }

  async stopApp(bundleId: string, opts?: CallOptions): Promise<void> {
    await this.tcp.call("forceStopApp", { bundleId }, { signal: opts?.signal });
    if (this.lastLaunchedBundleId === bundleId) {
      this.lastLaunchedBundleId = "";
    }
  }

  async killApp(bundleId: string, opts?: CallOptions): Promise<void> {
    // iOS distinction between stop and kill is not meaningful at
    // the XCUITest layer — both go through `forceStopApp`.
    await this.stopApp(bundleId, opts);
  }

  async currentForeground(_opts?: CallOptions): Promise<ForegroundInfo> {
    // The Swift driver tracks the last-launched bundle id; it
    // does NOT query the system for the "true" foreground app
    // (iOS sandboxing would require private APIs). That's fine
    // for test runs where the driver is the thing launching.
    return {
      bundleId: this.lastLaunchedBundleId || null,
    };
  }

  async listApps(_opts?: CallOptions): Promise<readonly InstalledApp[]> {
    // listApps on iOS is host-side — the Swift XCUITest runner has
    // no listApps command. We call xcrun directly from the host.
    if (this.opts.kind === "simulator") {
      return this.listAppsSimulator();
    }
    return this.listAppsDevice();
  }

  private async listAppsSimulator(): Promise<readonly InstalledApp[]> {
    try {
      // simctl listapps outputs plist; pipe through plutil for JSON.
      // UDID is a UUID — no shell injection risk.
      const { stdout } = await execAsync(
        `xcrun simctl listapps '${this.opts.udid}' | plutil -convert json -o - -- -`,
      );
      const parsed = JSON.parse(stdout) as Record<
        string,
        { CFBundleDisplayName?: string; CFBundleName?: string }
      >;
      return Object.entries(parsed).map(([bundleId, info]) => ({
        bundleId,
        displayName:
          info.CFBundleDisplayName ?? info.CFBundleName ?? bundleId,
      }));
    } catch {
      return [];
    }
  }

  private async listAppsDevice(): Promise<readonly InstalledApp[]> {
    try {
      const { stdout } = await execAsync(
        `xcrun devicectl device info apps --device '${this.opts.udid}' --json-output -`,
      );
      const parsed = JSON.parse(stdout) as {
        result?: {
          apps?: Array<{
            bundleIdentifier?: string;
            name?: string;
          }>;
        };
      };
      return (parsed.result?.apps ?? [])
        .filter(
          (a): a is { bundleIdentifier: string; name?: string } =>
            typeof a.bundleIdentifier === "string",
        )
        .map((a) => ({
          bundleId: a.bundleIdentifier,
          displayName: a.name ?? a.bundleIdentifier,
        }));
    } catch {
      return [];
    }
  }

  // ── Media + device info ─────────────────────────────────────

  async screenshot(opts?: CallOptions): Promise<Uint8Array> {
    const data = await this.tcp.call("screenshot", {}, { signal: opts?.signal });
    const base64 = (data.base64 as string) ?? "";
    return Buffer.from(base64, "base64");
  }

  async deviceInfo(_opts?: CallOptions): Promise<DeviceInfo> {
    return {
      platform: "ios",
      platformVersion: "unknown",
      model: "unknown",
      udid: this.opts.udid,
      kind: this.opts.kind === "simulator" ? "simulator" : "device",
    };
  }

  async screenSize(opts?: CallOptions): Promise<Size> {
    const data = await this.tcp.call("getScreenSize", {}, { signal: opts?.signal });
    return {
      width: (data.width as number) ?? 0,
      height: (data.height as number) ?? 0,
    };
  }
}
