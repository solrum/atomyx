export interface DeviceInfo {
  id: string;
  serial: string;
  platform: "android" | "ios";
  /**
   * For iOS, distinguishes a Simulator from a physical device. Affects
   * transport strategy: simulators share the host network namespace so
   * TCP localhost works directly; physical devices require `iproxy`
   * (libimobiledevice) tunneling to reach the driver's listen port
   * over USB. Undefined on Android (only physical devices via adb).
   */
  kind?: "sim" | "device";
  model?: string;
  state: string;
}

export interface RawElement {
  elementId: string;
  className?: string;
  resourceId?: string;
  text?: string;
  contentDesc?: string;
  bounds?: { left: number; top: number; right: number; bottom: number };
  clickable?: boolean;
  enabled?: boolean;
  children?: RawElement[];
}

/**
 * Platform-neutral element selector. Each field carries semantic meaning
 * independent of the platform — per-platform adapters (Android HTTP, iOS
 * XCTest) map these to their native query forms.
 *
 * TODO(ios): consider a `Selector<P extends Platform>` discriminated union
 * so iOS-only fields (`predicate`, `classChain`) are type-level gated to
 * the iOS adapter path. Today they're additive — Android adapter silently
 * ignores them, which works but loses the type-level guarantee that an
 * agent can't accidentally pass an iOS query to Android. Not blocking iOS
 * implementation; revisit after iOS adapter lands and we have real usage
 * data to inform the ergonomic tradeoff.
 *
 * Semantic mapping:
 *
 *   | field         | Android native                      | iOS native                              |
 *   | ------------- | ----------------------------------- | --------------------------------------- |
 *   | resourceId    | viewIdResourceName                  | accessibilityIdentifier                 |
 *   | contentDesc   | contentDescription                  | accessibilityLabel                      |
 *   | text          | text                                | value (StaticText) / label (Button)     |
 *   | textContains  | text substring                      | label CONTAINS (NSPredicate)            |
 *   | hint          | fuzzy fallback (any field)          | fuzzy fallback (any field)              |
 *   | predicate     | (ignored)                           | NSPredicate string                      |
 *   | classChain    | (ignored)                           | XCUITest class chain query              |
 *   | nth           | 0-based index when multiple match   | 0-based index when multiple match       |
 *
 * Priority order on both sides: resourceId > contentDesc > text > textContains
 * > hint. Elements without any stable selector should be addressed via
 * coordinates (tap({x,y}) / input_text({x,y,text})).
 *
 * `predicate` and `classChain` are iOS-only escape hatches for XCUITest's
 * native query language — they allow complex queries that don't map cleanly
 * to the primary fields. The Android adapter ignores them.
 */
export interface Selector {
  /** Stable id (Android viewIdResourceName / iOS accessibilityIdentifier). */
  resourceId?: string;
  /** Accessibility label (Android contentDescription / iOS accessibilityLabel). */
  contentDesc?: string;
  /** Visible text (Android text / iOS value or label). */
  text?: string;
  /** Substring match against text/label. */
  textContains?: string;
  /** Fuzzy fallback — matches any selector field loosely. */
  hint?: string;
  /** iOS-only: XCUITest NSPredicate string. Ignored by Android. */
  predicate?: string;
  /** iOS-only: XCUITest class chain query. Ignored by Android. */
  classChain?: string;
  /** 0-based index when multiple elements match the query. */
  nth?: number;
}

export interface ResolvedElement {
  found: boolean;
  /**
   * Which selector strategy produced the match. Adds `predicate` and
   * `classChain` for iOS-native escape-hatch queries (Android ignores
   * those fields so they never appear there).
   */
  resolvedBy?:
    | "resourceId"
    | "text"
    | "contentDesc"
    | "textContains"
    | "hint"
    | "predicate"
    | "classChain";
  bounds?: { left: number; top: number; right: number; bottom: number };
  resourceId?: string | null;
  text?: string | null;
  contentDesc?: string | null;
  className?: string | null;
  clickable?: boolean;
  enabled?: boolean;
  /** True if this element lives inside the IME window (soft keyboard). */
  isInIme?: boolean;
  /**
   * Populated by adapters that can detect view z-order — currently
   * iOS only via snapshot walk. If the resolved element's midpoint is
   * covered by another element (modal sheet, alert, toolbar), that
   * obscuring element's identity is reported here. Tap/inputText
   * callers use this to short-circuit with an actionable error
   * instead of dispatching a coordinate tap that would hit the
   * overlay. Absent on Android because accessibility click actions
   * bypass coordinate hit-testing and are not affected by overlays.
   */
  obscuredBy?: {
    role: string;
    identifier: string;
    label: string;
  };
}

export interface CompactElement {
  selector: Record<string, string>;
  label: string;
  role: string;
  clickable: boolean;
  enabled: boolean;
  bounds: { left: number; top: number; right: number; bottom: number };
  isInIme: boolean;
}

export interface KeyboardInfo {
  visible: boolean;
  packageName: string | null;
  layout: "numeric_pad" | "qwerty" | "alpha" | "phone_alpha" | "numeric_partial" | "unknown" | "none";
  bounds: { left: number; top: number; right: number; bottom: number } | null;
  keys: Array<{
    label: string;
    bounds: { left: number; top: number; right: number; bottom: number };
  }>;
}

export interface ActionResult {
  ok: boolean;
  reason?: string;
}

export interface TypeKeyboardResult {
  success: boolean;
  typed: number;
  total: number;
  reason: string;
}

/**
 * Lifecycle metadata about the device session.
 */
export interface DeviceLifecycle {
  readonly platform: "android" | "ios";
  readonly deviceId: string;
  dispose(): Promise<void>;
}

/**
 * Platform-neutral foreground descriptor.
 *
 *   - `appId`  → Android package name / iOS bundle id
 *   - `screen` → Android activity simple name / iOS view controller name
 *                / iOS route name. May be empty if the platform cannot
 *                introspect the current screen.
 */
export interface ForegroundInfo {
  appId: string;
  screen?: string;
}

/**
 * Read-only inspection of device state. No mutation.
 */
export interface DeviceInspector {
  getUiTree(): Promise<RawElement>;
  getUiSummary(): Promise<CompactElement[]>;
  resolveSelector(selector: Selector): Promise<ResolvedElement>;
  screenshot(): Promise<{ base64: string; format: "png" }>;
  getKeyboard(): Promise<KeyboardInfo>;
  /**
   * Current foreground app + screen. Platform-neutral wrapper over
   * Android's `{packageName, activity}` and iOS's `{bundleId, viewController}`.
   */
  currentForeground(): Promise<ForegroundInfo>;
}

/**
 * UI mutation actions: gestures, input, key presses.
 */
export interface DeviceActor {
  tap(selector: Selector): Promise<ActionResult>;
  tapCoordinates(x: number, y: number): Promise<void>;
  longPressCoordinates(x: number, y: number, durationMs?: number): Promise<void>;
  clearFocusedInput(): Promise<ActionResult>;
  swipe(fromX: number, fromY: number, toX: number, toY: number, durationMs?: number): Promise<void>;
  inputText(selector: Selector, text: string): Promise<ActionResult>;
  typeViaKeyboard(text: string, perKeyDelayMs?: number, clearFirst?: boolean): Promise<TypeKeyboardResult>;
  /**
   * Press a system key. Returns an `ActionResult` because the "back"
   * key is NOT a universal primitive — iOS has no system-level back,
   * only per-screen affordances (nav bar button, modal cancel button,
   * edge swipe, app-specific close button). Agents must check
   * `result.ok` before assuming navigation happened.
   *
   * Semantics per key:
   *
   *   - `home`  → device-level home press (`XCUIDevice.shared.press(.home)`
   *               on iOS, HOME keycode on Android). Always `ok: true`.
   *   - `enter` → type `\n` into the currently-focused field. No way to
   *               verify success (XCUITest / Android IME both swallow
   *               silently if no field focused). Always `ok: true`.
   *   - `back`  → **best-effort**. iOS adapter tries: (1) nav bar back
   *               button, (2) edge-swipe-from-left fallback. Returns
   *               `ok: true` only when a verifiable affordance was
   *               tapped. Edge swipe is unverifiable → `ok: false` with
   *               reason. Android adapter always returns `ok: true`
   *               because the system back intent is guaranteed to fire
   *               regardless of whether the app handles it.
   *
   * When `ok: false` on iOS back, agents should fall back to
   * `find_element(label IN {"Back", "Cancel", "Done", "Close"}) + tap`
   * to locate a screen-specific back affordance.
   */
  pressKey(key: "back" | "home" | "enter"): Promise<ActionResult>;
}

/**
 * Installed-app descriptor. `appId` is the platform-neutral id:
 * Android package name (`com.example.app`) or iOS bundle id
 * (`com.example.App`). `label` is the user-visible app name.
 */
export interface InstalledApp {
  appId: string;
  label?: string;
}

/**
 * App-level operations distinct from per-screen mutations.
 */
export interface DeviceAppManager {
  listApps(): Promise<InstalledApp[]>;
  launchApp(appId: string): Promise<void>;
  forceStopApp(appId: string): Promise<void>;
}

/**
 * Composite that all adapters implement. Tools depend on the smallest
 * sub-interface they need (Inspector, Actor, AppManager) — not the whole thing.
 */
export type DeviceController = DeviceLifecycle & DeviceInspector & DeviceActor & DeviceAppManager;
