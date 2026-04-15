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
  TreeNode,
} from "@atomyx/core-driver";
import { TcpClient } from "./tcp-client.js";
import { Iproxy } from "./iproxy.js";
import { normalizeIosTree, type IosRawElement } from "./tree-normalizer.js";

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
 *   - Selector resolution. The Swift `resolveSelector` command
 *     still exists on the driver side (legacy), but this host
 *     adapter never calls it. All selector logic lives in
 *     `@atomyx/core` via `compileSelector` + `Finder`.
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
}

export class IosDriver implements Driver {
  readonly platform = "ios" as const;
  readonly capabilities: Capabilities = {
    canScreenshot: true,
    canEraseText: true, // Swift driver exposes clearFocusedInput
    canWaitForIdle: false, // no native idle primitive yet — core polls
    canSetLocation: false,
    canSetOrientation: false,
    supportedKeyCodes: ["back", "home", "enter"],
  };

  private readonly port: number;
  private readonly tcp: TcpClient;
  private readonly iproxy: Iproxy | null;
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
  }

  // ── Lifecycle ────────────────────────────────────────────────

  async connect(): Promise<void> {
    if (this.iproxy) {
      await this.iproxy.start();
    }
    await this.tcp.connect();
    // Ping handshake verifies the Swift driver is up and speaking
    // our protocol — not just "a port is listening".
    await this.tcp.call("ping", {});
  }

  async disconnect(): Promise<void> {
    await this.tcp.disconnect();
    if (this.iproxy) {
      await this.iproxy.stop();
    }
    this.lastLaunchedBundleId = "";
  }

  isConnected(): boolean {
    return this.tcp.isConnected();
  }

  // ── Hierarchy ────────────────────────────────────────────────

  async hierarchy(): Promise<TreeNode> {
    const data = await this.tcp.call("dumpRawTree", {});
    const root = (data.root as IosRawElement | undefined) ?? {
      elementType: "other",
    };
    const wire = normalizeIosTree(root);
    return wire as unknown as TreeNode;
  }

  async waitForIdle(_timeoutMs: number): Promise<boolean> {
    // capabilities.canWaitForIdle === false; core should not
    // invoke this. Defensive no-op.
    return true;
  }

  // ── Gesture primitives ──────────────────────────────────────

  async tap(point: Point): Promise<void> {
    await this.tcp.call("tapAt", { x: point.x, y: point.y });
  }

  async longPress(point: Point, durationMs: number): Promise<void> {
    await this.tcp.call("longPressAt", { x: point.x, y: point.y, durationMs });
  }

  async swipe(from: Point, to: Point, durationMs: number): Promise<void> {
    await this.tcp.call("swipe", {
      fromX: from.x,
      fromY: from.y,
      toX: to.x,
      toY: to.y,
      durationMs,
    });
  }

  // ── Text input ──────────────────────────────────────────────

  async inputText(text: string): Promise<void> {
    // Uses iOS native `XCUIApplication.typeText()` under the hood
    // via the Swift `typeText` command. Requires a focused field.
    await this.tcp.call("typeText", { text });
  }

  async eraseText(count: number): Promise<void> {
    // Swift driver has `clearFocusedInput` which bulk-sends
    // delete keys to the focused field. Ignores the explicit
    // count — always clears everything. Close enough for the
    // framework's "erase before type" flow.
    await this.tcp.call("clearFocusedInput", { maxKeys: count });
  }

  async pressKey(key: KeyCode): Promise<KeyResult> {
    const data = await this.tcp.call("pressKey", { key });
    // Swift driver returns {ok, reason?} in the data envelope
    // for iOS `pressKey("back")` specifically — iOS has no
    // system back, the driver tries on-screen affordances.
    return {
      ok: data.ok !== false,
      reason: typeof data.reason === "string" ? data.reason : undefined,
    };
  }

  // ── App lifecycle ────────────────────────────────────────────

  async launchApp(bundleId: string, _args?: LaunchArgs): Promise<void> {
    await this.tcp.call("launchApp", { bundleId });
    this.lastLaunchedBundleId = bundleId;
  }

  async stopApp(bundleId: string): Promise<void> {
    await this.tcp.call("forceStopApp", { bundleId });
    if (this.lastLaunchedBundleId === bundleId) {
      this.lastLaunchedBundleId = "";
    }
  }

  async killApp(bundleId: string): Promise<void> {
    // iOS distinction between stop and kill is not meaningful at
    // the XCUITest layer — both go through `forceStopApp`.
    await this.stopApp(bundleId);
  }

  async currentForeground(): Promise<ForegroundInfo> {
    // The Swift driver tracks the last-launched bundle id; it
    // does NOT query the system for the "true" foreground app
    // (iOS sandboxing would require private APIs). That's fine
    // for test runs where the driver is the thing launching.
    return {
      bundleId: this.lastLaunchedBundleId || null,
    };
  }

  async listApps(): Promise<readonly InstalledApp[]> {
    // listApps on iOS is host-side — simctl for simulator,
    // devicectl for device. The Swift driver does NOT expose a
    // `listApps` command; this driver returns an empty list and
    // relies on the composition layer (CLI, MCP server) to
    // populate via direct xcrun invocation. Keeping the Driver
    // port honest: don't fabricate data we can't get through
    // the wire.
    return [];
  }

  // ── Media + device info ─────────────────────────────────────

  async screenshot(): Promise<Uint8Array> {
    const data = await this.tcp.call("screenshot", {});
    const base64 = (data.base64 as string) ?? "";
    return Buffer.from(base64, "base64");
  }

  async deviceInfo(): Promise<DeviceInfo> {
    return {
      platform: "ios",
      platformVersion: "unknown",
      model: "unknown",
      udid: this.opts.udid,
      kind: this.opts.kind === "simulator" ? "simulator" : "device",
    };
  }

  async screenSize(): Promise<Size> {
    const data = await this.tcp.call("getScreenSize", {});
    return {
      width: (data.width as number) ?? 0,
      height: (data.height as number) ?? 0,
    };
  }
}
