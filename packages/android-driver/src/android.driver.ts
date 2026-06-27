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
} from "@atomyx/driver";
import type { TreeNode } from "@atomyx/driver";
import { adbForward, adbForwardRemove } from "./adb.js";
import { ClearTextError } from "./clear/index.js";
import { HttpClient } from "./http-client.js";
import { type AndroidRawElement, normalizeAndroidTree } from "./tree-normalizer.js";

/**
 * Android `Driver` implementation over the Kotlin APK's HTTP
 * control server.
 *
 * Why this class is intentionally thin:
 *
 *   - Selector resolution, scroll-into-view, obscurement
 *     detection, and priority broadening all live in the core
 *     framework. This driver never sees a `Selector` object — it
 *     only receives coordinate primitives from the Orchestra
 *     layer.
 *
 *   - The APK exposes a device-side wire shape (resourceId /
 *     contentDesc / BoundsDto) that predates the canonical
 *     TreeNode shape the framework operates on. This adapter does
 *     the wire-to-canonical translation host-side in
 *     `normalizeAndroidTree`, keeping the translation boundary in
 *     exactly one file.
 *
 *   - Features not exposed by the APK (native waitForIdle,
 *     killApp, native screenSize) are reported via capability
 *     flags as unsupported; the core framework routes around them
 *     using host-side fallbacks (backspace loop, tree-diff
 *     polling).
 *
 * Lifecycle contract:
 *
 *   1. `connect()` — spawns `adb forward tcp:<hostPort> tcp:8765`
 *      and verifies the APK is reachable via `GET /health`.
 *      Throws on adb missing, device not authorized, APK not
 *      running, or port already forwarded.
 *
 *   2. Driver methods dispatch HTTP requests; any non-200
 *      response bubbles up as `HttpClientError`.
 *
 *   3. `disconnect()` — best-effort `adb forward --remove`.
 *      Never throws; idempotent.
 */

/**
 * Per-request timeout for `dispatchGesture`. The APK waits on the
 * AccessibilityService completion callback (`GestureResultCallback`)
 * which in turn waits for the gesture to fully replay. Set to 35s
 * to give the APK's 30s `CountDownLatch` room to either fire or
 * time out on its own — losing the race at the HTTP layer would
 * surface as a generic timeout instead of the APK's structured
 * `gesture_timed_out` error.
 */
const GESTURE_REQUEST_TIMEOUT_MS = 35_000;

export interface AndroidDriverOptions {
  /** Android device serial from `adb devices`. */
  readonly serial: string;
  /**
   * Host-side TCP port to forward. Defaults to 8765 (same as
   * the device-side APK port). Change when multiple drivers
   * run concurrently — each device needs a distinct host port.
   */
  readonly hostPort?: number;
  /** Device-side port the APK listens on. Default 8765. */
  readonly devicePort?: number;
  /** Per-request HTTP timeout. Default 10s. */
  readonly requestTimeoutMs?: number;
}

export class AndroidDriver implements Driver {
  readonly platform = "android" as const;

  /**
   * Capabilities populated at `connect()` time from the APK's
   * `/ping` response. Gesture capabilities (`canMultiPointer`,
   * `canPressure`) reflect what the APK's active gesture runner
   * can dispatch — flipped once a multi-stroke backend ships.
   * Everything else is a static property of this driver build.
   *
   * Until `/ping` resolves, the defaults are conservative — a
   * failed ping falls back to the same single-pointer / no-
   * pressure surface the APK has shipped to date.
   */
  private _capabilities: Capabilities = {
    canScreenshot: true,
    canEraseText: true, // backed by APK's /actions/clear_focused_input route
    canWaitForIdle: false, // host-side tree-diff fallback only
    canSetLocation: false,
    canSetOrientation: false,
    canHideKeyboard: true, // backed by APK's /actions/hide_keyboard route
    canMultiPointer: false,
    canPressure: false,
    supportedKeyCodes: ["back", "home", "enter"],
  };

  get capabilities(): Capabilities {
    return this._capabilities;
  }

  /**
   * Internal mechanism the APK uses to dispatch gestures. Captured
   * from the `/ping` response for log correlation only — callers
   * MUST NOT branch on this. Branch on `capabilities` instead.
   * `null` until `connect()` runs or when `/ping` is unreachable.
   */
  private _gestureMechanism: string | null = null;

  get gestureMechanism(): string | null {
    return this._gestureMechanism;
  }

  private readonly hostPort: number;
  private readonly devicePort: number;
  private readonly http: HttpClient;
  private connected = false;

  constructor(private readonly opts: AndroidDriverOptions) {
    this.hostPort = opts.hostPort ?? 8765;
    this.devicePort = opts.devicePort ?? 8765;
    this.http = new HttpClient({
      baseUrl: `http://127.0.0.1:${this.hostPort}`,
      defaultTimeoutMs: opts.requestTimeoutMs ?? 10_000,
    });
  }

  // ── Lifecycle ────────────────────────────────────────────────

  async connect(opts?: CallOptions): Promise<void> {
    await adbForward(this.opts.serial, this.hostPort, this.devicePort);
    // Liveness probe. `/health` returns
    // `{ok, accessibilityConnected}` — we only care that the APK
    // responds; the accessibility state is advisory.
    await this.http.get<{ ok: boolean }>("/health", { signal: opts?.signal });
    // Capability handshake. `/ping` populates gesture capabilities
    // (`canMultiPointer`, `canPressure`) and the active mechanism
    // name so the YAML validator can reject unsupported gesture
    // shapes before they leave the host. A pre-capability APK
    // (older build) returns 404 — we swallow that and keep the
    // conservative defaults so old agents still work.
    try {
      const pong = await this.http.get<Record<string, unknown>>("/ping", {
        signal: opts?.signal,
      });
      this.applyPingCapabilities(pong);
    } catch {
      // Older agents pre-date /ping; keep conservative defaults.
      this._gestureMechanism = null;
    }
    this.connected = true;
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
    this._gestureMechanism = typeof mechanism === "string" ? mechanism : null;
  }

  async disconnect(): Promise<void> {
    if (!this.connected) return;
    await adbForwardRemove(this.opts.serial, this.hostPort);
    this.connected = false;
  }

  isConnected(): boolean {
    return this.connected;
  }

  /**
   * Reconnect after a stale binding (APK restart, adb forward
   * dropped, accessibility service toggled off/on). Re-issues
   * `adb forward`, re-verifies `/health`, and re-runs the `/ping`
   * handshake so capability drift — e.g. an APK upgrade that
   * flips `canMultiPointer` — is picked up instead of silently
   * dispatching against stale flags.
   *
   * Throws with an actionable error when `/ping` fails: a forward
   * to a port that happens to be listening (another agent, stale
   * tunnel) would otherwise silently succeed but route commands
   * to the wrong target.
   */
  async reconnect(): Promise<void> {
    // `adb forward` is idempotent when the forward is already in
    // place; re-issuing costs nothing and rebinds on a dropped
    // tunnel. Safer than checking first and racing the gap.
    await adbForward(this.opts.serial, this.hostPort, this.devicePort);
    await this.http.get<{ ok: boolean }>("/health");
    try {
      const pong = await this.http.get<Record<string, unknown>>("/ping");
      this.applyPingCapabilities(pong);
    } catch (err) {
      throw new Error(
        `Android driver reconnect reached /health but /ping failed — ` +
          `the agent may have been downgraded to a pre-capability build, ` +
          `or adb forward is routing to a stale listener. Try: restart ` +
          `the Atomyx foreground service on the device and reconnect. ` +
          `(${(err as Error).message})`,
      );
    }
    this.connected = true;
  }

  // ── Hierarchy ────────────────────────────────────────────────

  async hierarchy(opts?: CallOptions): Promise<TreeNode> {
    const raw = await this.http.get<AndroidRawElement>("/tree", {
      signal: opts?.signal,
    });
    const wire = normalizeAndroidTree(raw);
    // TreeNodeWire is structurally compatible with TreeNode from
    // @atomyx/driver — both have `attributes`, `children`, and the
    // optional state booleans. Cast is safe and documented as
    // the shape-identity of the two types.
    return wire as unknown as TreeNode;
  }

  async waitForIdle(_timeoutMs: number, _opts?: CallOptions): Promise<boolean> {
    // Capability says false — core should not call this. Guarded
    // as a defensive no-op so a misbehaving consumer gets a
    // truthy return rather than a hang.
    return true;
  }

  // ── Gesture primitives ──────────────────────────────────────

  async tap(point: Point, opts?: CallOptions): Promise<void> {
    await this.http.post(
      "/actions/tap_coords",
      { x: point.x, y: point.y },
      { signal: opts?.signal },
    );
  }

  async longPress(point: Point, durationMs: number, opts?: CallOptions): Promise<void> {
    await this.http.post(
      "/actions/long_press",
      { x: point.x, y: point.y, durationMs },
      { signal: opts?.signal },
    );
  }

  async swipe(
    from: Point,
    to: Point,
    durationMs: number,
    opts?: CallOptions,
  ): Promise<void> {
    await this.http.post(
      "/actions/swipe",
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

  async dispatchGesture(gesture: Gesture, opts?: CallOptions): Promise<void> {
    // Capability gate — host-side first tier. A matching guard
    // lives inside the APK (`DispatchGestureRoute`) so a driver
    // that bypasses this class still cannot leak an unsupported
    // gesture into the AccessibilityService.
    if (gesture.pointers.length === 0) {
      throw new Error("dispatchGesture: empty pointers array");
    }
    if (gesture.pointers.length > 1 && !this.capabilities.canMultiPointer) {
      throw new Error(
        `Android driver cannot dispatch ${gesture.pointers.length}-pointer gesture: ` +
          `multi-pointer capability is unavailable. Multi-stroke dispatch via ` +
          `AccessibilityService.GestureDescription with continueStroke requires ` +
          `API 33+ and a future APK route; the current build is single-pointer only.`,
      );
    }
    for (const p of gesture.pointers) {
      for (const w of p.waypoints) {
        if (w.pressure !== undefined && !this.capabilities.canPressure) {
          throw new Error(
            `Android driver cannot dispatch pressure waypoints: the ` +
              `AccessibilityService gesture surface does not expose per-touch ` +
              `pressure injection. Drop the pressure field or use iOS.`,
          );
        }
      }
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
    // APK-side gesture dispatch waits on the AccessibilityService
    // completion callback (typical gesture 200-800ms, reorder up
    // to ~1.5s). Widen the request budget so a legitimate long
    // drag doesn't spuriously fail the default 10s HTTP timeout.
    await this.http.post(
      "/actions/dispatch_gesture",
      { pointers },
      { timeoutMs: GESTURE_REQUEST_TIMEOUT_MS, signal: opts?.signal },
    );
  }

  // ── Input ────────────────────────────────────────────────────

  async inputText(text: string, opts?: CallOptions): Promise<void> {
    // Invariant: `inputText` APPENDS. The APK's
    // `/actions/type_keyboard` route defaults `clearFirst` to true
    // when the parameter is omitted, so we must pass
    // `clearFirst: false` explicitly to honor the Driver port
    // contract. Clearing is a separate step the caller drives via
    // `eraseText` when intended.
    await this.http.post(
      "/actions/type_keyboard",
      { text, clearFirst: false },
      { signal: opts?.signal },
    );
  }

  async eraseText(_count: number, opts?: CallOptions): Promise<void> {
    // APK's `/actions/clear_focused_input` clears the focused field via
    // a four-strategy chain. `count` is ignored — the route always
    // clears the entire current value. On full failure the APK returns
    // `ok: false`; we throw ClearTextError so callers can distinguish a
    // clear failure from a transport error.
    const res = await this.http.post<{
      ok: boolean;
      strategiesTried?: string[];
      lastValue?: string;
      focusedNodeDesc?: string;
      screenWidth?: number;
      screenHeight?: number;
    }>("/actions/clear_focused_input", {}, { signal: opts?.signal });
    if (!res.ok) {
      throw new ClearTextError({
        strategiesTried: res.strategiesTried ?? [],
        lastValue: res.lastValue ?? "",
        focusedNodeDesc: res.focusedNodeDesc ?? "unknown",
        screenWidth: res.screenWidth ?? 0,
        screenHeight: res.screenHeight ?? 0,
      });
    }
  }

  async pressKey(key: KeyCode, opts?: CallOptions): Promise<KeyResult> {
    await this.http.post("/actions/key", { key }, { signal: opts?.signal });
    // Android system key dispatch always "fires" regardless of
    // whether the app handled it. Report ok unconditionally.
    return { ok: true };
  }

  async hideKeyboard(opts?: CallOptions): Promise<KeyResult> {
    const res = await this.http.post<{ ok: boolean; reason?: string }>(
      "/actions/hide_keyboard",
      {},
      { signal: opts?.signal },
    );
    return { ok: res.ok, reason: res.reason };
  }

  // ── App lifecycle ────────────────────────────────────────────

  async launchApp(
    bundleId: string,
    _args?: LaunchArgs,
    opts?: CallOptions,
  ): Promise<void> {
    await this.http.post(
      "/actions/launch",
      { packageName: bundleId },
      { signal: opts?.signal },
    );
  }

  async stopApp(bundleId: string, opts?: CallOptions): Promise<void> {
    await this.http.post(
      "/actions/force_stop",
      { packageName: bundleId },
      { signal: opts?.signal },
    );
  }

  async killApp(bundleId: string, opts?: CallOptions): Promise<void> {
    // Same endpoint as stopApp on Android — there's no hard
    // distinction at the AccessibilityService layer.
    await this.http.post(
      "/actions/force_stop",
      { packageName: bundleId },
      { signal: opts?.signal },
    );
  }

  async currentForeground(opts?: CallOptions): Promise<ForegroundInfo> {
    const raw = await this.http.get<{ packageName: string; activity?: string }>(
      "/current-activity",
      { signal: opts?.signal },
    );
    return {
      bundleId: raw.packageName || null,
      activity: raw.activity,
    };
  }

  async listApps(opts?: CallOptions): Promise<readonly InstalledApp[]> {
    const raw = await this.http.get<Array<{ packageName: string; label?: string }>>(
      "/apps",
      { signal: opts?.signal },
    );
    return raw.map((a) => ({
      bundleId: a.packageName,
      displayName: a.label ?? a.packageName,
    }));
  }

  // ── Media + device info ─────────────────────────────────────

  async screenshot(opts?: CallOptions): Promise<Uint8Array> {
    const raw = await this.http.get<{ base64: string; format: string }>(
      "/screenshot",
      { signal: opts?.signal },
    );
    return Buffer.from(raw.base64, "base64");
  }

  async deviceInfo(_opts?: CallOptions): Promise<DeviceInfo> {
    // The APK does not expose a device-info route; return a stub populated from known options.
    // Return a minimal stub populated from the serial we know.
    return {
      platform: "android",
      platformVersion: "unknown",
      model: "unknown",
      udid: this.opts.serial,
      kind: this.opts.serial.startsWith("emulator-") ? "emulator" : "device",
    };
  }

  async screenSize(opts?: CallOptions): Promise<Size> {
    // The APK does not expose /screen-size directly. The `/tree` response
    // has a synthetic `el_root` wrapper with no bounds of its own —
    // it's just a container that holds the actual window roots
    // (status bar excluded, main window, optional IME window).
    //
    // The main window's root is the first top-level child with
    // bounds populated. Using its width/height gives us screen
    // dimensions without adding a new route to the APK or shelling
    // out to `adb shell wm size`.
    //
    // Invariant: the Kotlin `UiTreeService.dumpTree` returns a
    // root DTO with NO bounds attribute. Always traverse to a
    // child with bounds — never read `tree.attributes.bounds`
    // directly.
    const tree = await this.hierarchy(opts);
    const bounds = findScreenBounds(tree);
    if (!bounds) {
      throw new Error(
        "screenSize: no child of the root has bounds — cannot derive screen dimensions",
      );
    }
    return {
      width: bounds.right - bounds.left,
      height: bounds.bottom - bounds.top,
    };
  }
}

/**
 * Walk the first layer of children looking for bounds. Returns the
 * LARGEST child bounds by area — the main window dominates over any
 * IME or overlay child — or null if none have bounds. The wire
 * format stores bounds as a "l,t,r,b" string on the `bounds`
 * attribute; we parse inline instead of importing the core
 * `parseBounds` helper to keep this package's dependency surface
 * minimal (driver packages deliberately avoid cross-package runtime
 * imports; types only).
 */
function findScreenBounds(
  tree: TreeNode,
): { left: number; top: number; right: number; bottom: number } | null {
  let best: { left: number; top: number; right: number; bottom: number } | null = null;
  let bestArea = 0;
  for (const child of tree.children) {
    const raw = child.attributes["bounds"];
    if (!raw) continue;
    const parts = raw.split(",").map((n) => Number(n));
    if (parts.length !== 4 || parts.some((n) => !Number.isFinite(n))) continue;
    const [l, t, r, b] = parts as [number, number, number, number];
    const area = (r - l) * (b - t);
    if (area > bestArea) {
      bestArea = area;
      best = { left: l, top: t, right: r, bottom: b };
    }
  }
  return best;
}
