export interface DeviceInfo {
  id: string;
  serial: string;
  platform: "android" | "ios";
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
  resolvedBy?: "resourceId" | "text" | "contentDesc" | "textContains" | "hint";
  bounds?: { left: number; top: number; right: number; bottom: number };
  resourceId?: string | null;
  text?: string | null;
  contentDesc?: string | null;
  className?: string | null;
  clickable?: boolean;
  enabled?: boolean;
  /** True if this element lives inside the IME window (soft keyboard). */
  isInIme?: boolean;
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
   * Press a system key.
   *
   * TODO(ios): `"back"` is Android-only (hardware / software back button).
   * iOS has no back button; the equivalent gesture is a swipe-from-left-edge.
   * When iOS lands, either (a) rename to a platform-neutral `navigate({direction: "back"|"forward"})`
   * that the iOS adapter maps to a swipe gesture, or (b) have the iOS
   * adapter throw a clear "use swipe instead" error on pressKey("back").
   * `"home"` maps cleanly on both platforms. `"enter"` is IME-specific.
   */
  pressKey(key: "back" | "home" | "enter"): Promise<void>;
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
