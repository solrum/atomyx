import type { TreeNode } from "../tree/tree-node.js";

/**
 * Primitive device surface. Each concrete driver (iOS, Android,
 * future Web) implements this interface as a thin wrapper over its
 * native control channel. Selector resolution, scroll-into-view,
 * obscurement detection, retry, and any cross-platform ergonomics
 * live in the core framework — NOT here.
 *
 * Rule of thumb: if a method can be implemented in TypeScript using
 * other Driver methods, it does NOT belong on this interface.
 * Example: `tap(selector)` is composable from `hierarchy()` +
 * `tap(point)` and therefore stays in `core/selectors`, not here.
 *
 * Capability flags (see {@link Capabilities}) cover optional
 * features. Core MUST check `capabilities.canX` before invoking a
 * conditionally-supported method and fall back to a portable
 * implementation when the driver returns false.
 */
export interface Driver {
  /** Platform identifier — "ios", "android", or a future platform. */
  readonly platform: string;

  /** Static capability flags for this driver instance. */
  readonly capabilities: Capabilities;

  // ── Lifecycle ───────────────────────────────────────────────

  /** Open the transport, perform handshake, reach ready state. */
  connect(): Promise<void>;

  /** Close transport, release resources. Idempotent. */
  disconnect(): Promise<void>;

  /** True when the transport is connected and ready to serve calls. */
  isConnected(): boolean;

  // ── Hierarchy & state ───────────────────────────────────────

  /**
   * Capture the current UI hierarchy. Drivers MUST return a fully
   * normalized tree (see `AttrKeys` for canonical key set). No
   * platform-native field names may leak to the caller.
   */
  hierarchy(): Promise<TreeNode>;

  /**
   * Block until the UI reaches an idle state or the timeout
   * expires. Returns `true` when idle was reached, `false` on
   * timeout.
   *
   * ONLY callable when `capabilities.canWaitForIdle === true`;
   * otherwise core falls back to tree-diff polling and this method
   * is not invoked.
   */
  waitForIdle(timeoutMs: number): Promise<boolean>;

  // ── Gesture primitives (coordinate-only) ────────────────────

  tap(point: Point): Promise<void>;

  longPress(point: Point, durationMs: number): Promise<void>;

  swipe(from: Point, to: Point, durationMs: number): Promise<void>;

  // ── Text input primitives ───────────────────────────────────

  /** Type `text` into whatever element currently has focus. */
  inputText(text: string): Promise<void>;

  /**
   * Erase `count` characters backwards from the focused field.
   * Drivers without native erase support return via
   * `capabilities.canEraseText === false`; core falls back to
   * issuing the platform's backspace key `count` times.
   */
  eraseText(count: number): Promise<void>;

  /**
   * Press a system or navigation key. The result indicates
   * whether a verifiable affordance was used — iOS has no system
   * back primitive, so on iOS this may return `ok:false` with a
   * hint to fall back to an on-screen Cancel/Close button via
   * the core finder.
   */
  pressKey(key: KeyCode): Promise<KeyResult>;

  // ── App lifecycle ───────────────────────────────────────────

  launchApp(bundleId: string, args?: LaunchArgs): Promise<void>;

  stopApp(bundleId: string): Promise<void>;

  killApp(bundleId: string): Promise<void>;

  currentForeground(): Promise<ForegroundInfo>;

  listApps(): Promise<readonly InstalledApp[]>;

  // ── Media & device info ─────────────────────────────────────

  /** PNG-encoded screenshot of the current screen. */
  screenshot(): Promise<Uint8Array>;

  deviceInfo(): Promise<DeviceInfo>;

  /** Logical screen size in points (NOT pixels). */
  screenSize(): Promise<Size>;
}

/**
 * Static capability flags. Used by core to decide whether to
 * invoke optional driver methods or fall back to a portable
 * host-side implementation.
 */
export interface Capabilities {
  readonly canScreenshot: boolean;
  readonly canEraseText: boolean;
  readonly canWaitForIdle: boolean;
  readonly canSetLocation: boolean;
  readonly canSetOrientation: boolean;
  readonly supportedKeyCodes: readonly KeyCode[];
}

export interface Point {
  readonly x: number;
  readonly y: number;
}

export interface Size {
  readonly width: number;
  readonly height: number;
}

/**
 * Cross-platform key vocabulary. Core uses these names; drivers
 * translate to platform-native key codes internally.
 *
 * Strings outside this enum are allowed for driver-specific keys
 * (Android KEYCODE_ constants, iOS hardware keys); consumers who
 * use them sacrifice portability knowingly.
 */
export type KeyCode =
  | "back"
  | "home"
  | "enter"
  | "tab"
  | "escape"
  | "delete"
  | "space"
  | "up"
  | "down"
  | "left"
  | "right"
  | (string & {});

export interface KeyResult {
  readonly ok: boolean;
  /**
   * Freeform reason — e.g. "used: nav_bar_back" on iOS success,
   * or "no system back on iOS — use find_element on Close/Cancel"
   * on iOS failure.
   */
  readonly reason?: string;
}

export interface LaunchArgs {
  readonly args?: readonly string[];
  readonly environment?: Readonly<Record<string, string>>;
}

export interface ForegroundInfo {
  readonly bundleId: string | null;
  /** Android activity name when available; undefined on iOS. */
  readonly activity?: string;
}

export interface InstalledApp {
  readonly bundleId: string;
  readonly displayName: string;
}

export interface DeviceInfo {
  readonly platform: string;
  readonly platformVersion: string;
  readonly model: string;
  readonly udid: string;
  readonly kind: "simulator" | "emulator" | "device";
}
