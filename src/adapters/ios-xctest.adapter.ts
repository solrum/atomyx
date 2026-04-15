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
 * iOS adapter — STUB. All methods throw "not implemented".
 *
 * The bridge approach is an open design question (see docs/ios.md). Candidate
 * implementations:
 *
 *   1. WebDriverAgent (Facebook / Appium's choice) — XCTest target running
 *      on the device, HTTP server, re-signed per install.
 *   2. Custom mini-WDA — smaller XCTest target exposing only the endpoints
 *      adet needs.
 *   3. Appium + iOS driver — full Appium stack wrapping WDA.
 *   4. simctl-only (simulator) — zero code signing, good for dev iteration.
 *
 * **Selector field mapping for iOS**:
 *
 *   - `Selector.resourceId`  → `accessibilityIdentifier` (set in Swift via
 *                              `view.accessibilityIdentifier = "..."`)
 *   - `Selector.contentDesc` → `accessibilityLabel`
 *   - `Selector.text`        → `value` (for StaticText) or `label` (for Button)
 *   - `Selector.textContains`→ NSPredicate `label CONTAINS[cd] "X"`
 *   - `Selector.hint`        → fuzzy any-field match
 *   - `Selector.predicate`   → raw NSPredicate string, passed through
 *   - `Selector.classChain`  → raw XCUITest class chain, passed through
 *
 * The tool layer already speaks this abstraction — iOS implementation
 * mainly needs to translate these fields into the chosen bridge's native
 * query format. Do NOT rename the Selector fields; they are semantic.
 *
 * TODO(ios): consider introducing an explicit `SelectorAdapter<P>` class
 * (e.g. `IosSelectorAdapter`) that owns the `Selector → native` mapping,
 * so the translation layer is unit-testable in isolation. Today the
 * Android adapter does the mapping implicitly at the HTTP wire boundary
 * (appId → packageName, etc). When iOS has non-trivial per-field
 * mappings (e.g. `text` → `value` vs `label` depending on element kind),
 * an adapter class will be cleaner than inlining the logic in each
 * Inspector / Actor method. Not required for Phase 1 iOS; revisit once
 * the bridge approach is chosen.
 */
export class IosXctestController implements DeviceController {
  readonly platform = "ios" as const;

  constructor(readonly deviceId: string) {}

  static async connect(deviceId: string): Promise<IosXctestController> {
    throw new Error(
      `iOS adapter not implemented yet (deviceId=${deviceId}). See docs/ios.md for design options.`,
    );
  }

  private nope(method: string): never {
    throw new Error(`ios.${method} not implemented — see docs/ios.md`);
  }

  // Inspector
  getUiTree(): Promise<RawElement> { return this.nope("getUiTree"); }
  getUiSummary(): Promise<CompactElement[]> { return this.nope("getUiSummary"); }
  resolveSelector(_selector: Selector): Promise<ResolvedElement> { return this.nope("resolveSelector"); }
  screenshot(): Promise<{ base64: string; format: "png" }> { return this.nope("screenshot"); }
  getKeyboard(): Promise<KeyboardInfo> { return this.nope("getKeyboard"); }
  currentForeground(): Promise<ForegroundInfo> { return this.nope("currentForeground"); }

  // Actor
  tap(_selector: Selector): Promise<ActionResult> { return this.nope("tap"); }
  tapCoordinates(_x: number, _y: number): Promise<void> { return this.nope("tapCoordinates"); }
  longPressCoordinates(_x: number, _y: number, _durationMs?: number): Promise<void> { return this.nope("longPressCoordinates"); }
  clearFocusedInput(): Promise<ActionResult> { return this.nope("clearFocusedInput"); }
  swipe(): Promise<void> { return this.nope("swipe"); }
  inputText(_selector: Selector, _text: string): Promise<ActionResult> { return this.nope("inputText"); }
  typeViaKeyboard(_text: string, _perKeyDelayMs?: number, _clearFirst?: boolean): Promise<TypeKeyboardResult> { return this.nope("typeViaKeyboard"); }
  pressKey(): Promise<void> { return this.nope("pressKey"); }

  // AppManager
  listApps(): Promise<InstalledApp[]> { return this.nope("listApps"); }
  launchApp(_appId: string): Promise<void> { return this.nope("launchApp"); }
  forceStopApp(_appId: string): Promise<void> { return this.nope("forceStopApp"); }

  async dispose(): Promise<void> {}
}
