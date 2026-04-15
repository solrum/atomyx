import type {
  Capabilities,
  DeviceInfo,
  Driver,
  ForegroundInfo,
  InstalledApp,
  KeyCode,
  KeyResult,
  LaunchArgs,
  Point,
  Size,
} from "@atomyx/core-driver";
import type { TreeNode } from "@atomyx/core-driver";
import { adbForward, adbForwardRemove } from "./adb.js";
import { HttpClient } from "./http-client.js";
import { type AndroidRawElement, normalizeAndroidTree } from "./tree-normalizer.js";

/**
 * Atomyx Android Driver — implements the `Driver` port over the
 * existing Kotlin APK HTTP control server.
 *
 * Why this class is intentionally thin:
 *
 *   - Selector resolution, scroll-into-view, obscurement
 *     detection, and priority broadening ALL live in
 *     `@atomyx/core`. This driver never sees a `Selector`
 *     object — it only receives coordinate primitives from the
 *     Orchestra layer.
 *
 *   - The Kotlin APK still exposes its legacy wire shape
 *     (resourceId / contentDesc / BoundsDto) because we haven't
 *     migrated the APK to the canonical `/hierarchy` route yet.
 *     This adapter does the legacy → canonical translation
 *     host-side in `normalizeAndroidTree`. When the APK is
 *     migrated to the canonical wire protocol, this adapter
 *     becomes a thin wrapper and most of the route-specific
 *     code below goes away.
 *
 *   - Features the legacy APK doesn't yet expose (eraseText,
 *     waitForIdle native, killApp, getScreenSize native) are
 *     reported via capability flags as unsupported. The core
 *     framework routes around them using host-side fallbacks
 *     (e.g. press backspace N times, tree-diff polling).
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
  readonly capabilities: Capabilities = {
    canScreenshot: true,
    canEraseText: false, // legacy APK doesn't expose a native erase route; core falls back
    canWaitForIdle: false, // host-side tree-diff fallback only
    canSetLocation: false,
    canSetOrientation: false,
    supportedKeyCodes: ["back", "home", "enter"],
  };

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

  async connect(): Promise<void> {
    await adbForward(this.opts.serial, this.hostPort, this.devicePort);
    // Verify the APK is up. The legacy /health returns {ok, accessibilityConnected}.
    await this.http.get<{ ok: boolean }>("/health");
    this.connected = true;
  }

  async disconnect(): Promise<void> {
    if (!this.connected) return;
    await adbForwardRemove(this.opts.serial, this.hostPort);
    this.connected = false;
  }

  isConnected(): boolean {
    return this.connected;
  }

  // ── Hierarchy ────────────────────────────────────────────────

  async hierarchy(): Promise<TreeNode> {
    const raw = await this.http.get<AndroidRawElement>("/tree");
    const wire = normalizeAndroidTree(raw);
    // TreeNodeWire is structurally compatible with TreeNode from
    // @atomyx/core-driver — both have `attributes`, `children`, and the
    // optional state booleans. Cast is safe and documented as
    // the shape-identity of the two types.
    return wire as unknown as TreeNode;
  }

  async waitForIdle(_timeoutMs: number): Promise<boolean> {
    // Capability says false — core should not call this. Guarded
    // as a defensive no-op so a misbehaving consumer gets a
    // truthy return rather than a hang.
    return true;
  }

  // ── Gesture primitives ──────────────────────────────────────

  async tap(point: Point): Promise<void> {
    await this.http.post("/actions/tap_coords", { x: point.x, y: point.y });
  }

  async longPress(point: Point, durationMs: number): Promise<void> {
    await this.http.post("/actions/long_press", {
      x: point.x,
      y: point.y,
      durationMs,
    });
  }

  async swipe(from: Point, to: Point, durationMs: number): Promise<void> {
    await this.http.post("/actions/swipe", {
      fromX: from.x,
      fromY: from.y,
      toX: to.x,
      toY: to.y,
      durationMs,
    });
  }

  // ── Input ────────────────────────────────────────────────────

  async inputText(text: string): Promise<void> {
    await this.http.post("/actions/type_keyboard", { text });
  }

  async eraseText(count: number): Promise<void> {
    // Legacy APK has no native erase route — fall back to
    // pressing back/delete via /actions/key. Core should route
    // around this via `capabilities.canEraseText === false`;
    // here we honor a direct call anyway.
    for (let i = 0; i < count; i++) {
      await this.http.post("/actions/key", { key: "delete" });
    }
  }

  async pressKey(key: KeyCode): Promise<KeyResult> {
    await this.http.post("/actions/key", { key });
    // Android system key dispatch always "fires" regardless of
    // whether the app handled it. Report ok unconditionally.
    return { ok: true };
  }

  // ── App lifecycle ────────────────────────────────────────────

  async launchApp(bundleId: string, _args?: LaunchArgs): Promise<void> {
    await this.http.post("/actions/launch", { packageName: bundleId });
  }

  async stopApp(bundleId: string): Promise<void> {
    await this.http.post("/actions/force_stop", { packageName: bundleId });
  }

  async killApp(bundleId: string): Promise<void> {
    // Same endpoint as stopApp on Android — there's no hard
    // distinction at the AccessibilityService layer.
    await this.http.post("/actions/force_stop", { packageName: bundleId });
  }

  async currentForeground(): Promise<ForegroundInfo> {
    const raw = await this.http.get<{ packageName: string; activity?: string }>(
      "/current-activity",
    );
    return {
      bundleId: raw.packageName || null,
      activity: raw.activity,
    };
  }

  async listApps(): Promise<readonly InstalledApp[]> {
    const raw = await this.http.get<Array<{ packageName: string; label?: string }>>(
      "/apps",
    );
    return raw.map((a) => ({
      bundleId: a.packageName,
      displayName: a.label ?? a.packageName,
    }));
  }

  // ── Media + device info ─────────────────────────────────────

  async screenshot(): Promise<Uint8Array> {
    const raw = await this.http.get<{ base64: string; format: "png" }>("/screenshot");
    return Buffer.from(raw.base64, "base64");
  }

  async deviceInfo(): Promise<DeviceInfo> {
    // Legacy APK doesn't expose a device-info route yet.
    // Return a minimal stub populated from the serial we know.
    return {
      platform: "android",
      platformVersion: "unknown",
      model: "unknown",
      udid: this.opts.serial,
      kind: this.opts.serial.startsWith("emulator-") ? "emulator" : "device",
    };
  }

  async screenSize(): Promise<Size> {
    // Legacy APK doesn't expose /screen-size. Derive from the
    // root bounds of the current hierarchy — every Android tree
    // has a root with screen-sized bounds in the accessibility
    // service.
    const tree = await this.hierarchy();
    const bounds = tree.attributes.bounds;
    if (!bounds) {
      throw new Error(
        "screenSize: root node has no bounds attribute — cannot derive screen dimensions",
      );
    }
    const [l, t, r, b] = bounds.split(",").map((n) => Number(n));
    if (!Number.isFinite(l) || !Number.isFinite(t) || !Number.isFinite(r) || !Number.isFinite(b)) {
      throw new Error(`screenSize: invalid bounds string "${bounds}"`);
    }
    return { width: (r ?? 0) - (l ?? 0), height: (b ?? 0) - (t ?? 0) };
  }
}
