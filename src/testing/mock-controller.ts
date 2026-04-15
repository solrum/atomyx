/**
 * In-memory mock DeviceController for unit tests. Records every call,
 * returns canned responses. No device required.
 *
 * Usage:
 *   const ctl = new MockController();
 *   ctl.queueTreeResponse({ elementId: "el_root", children: [] });
 *   const result = await ctl.tap({ contentDesc: "Login" });
 *   expect(ctl.calls).toContain({ method: "tap", args: { selector: ... } });
 */

import type {
  ActionResult,
  CompactElement,
  DeviceController,
  KeyboardInfo,
  RawElement,
  ResolvedElement,
  Selector,
  TypeKeyboardResult,
} from "../adapters/device-controller.port.js";

export interface MockCall {
  method: string;
  args: unknown;
}

export class MockController implements DeviceController {
  readonly platform = "android" as const;
  readonly deviceId = "mock-device";

  readonly calls: MockCall[] = [];

  private treeQueue: RawElement[] = [];
  private summaryQueue: CompactElement[][] = [];
  private resolveQueue: ResolvedElement[] = [];
  private tapResultQueue: ActionResult[] = [];
  private inputResultQueue: ActionResult[] = [];

  // ── canned response API ────────────────────────────────────────

  queueTreeResponse(tree: RawElement): this {
    this.treeQueue.push(tree);
    return this;
  }

  queueSummaryResponse(elements: CompactElement[]): this {
    this.summaryQueue.push(elements);
    return this;
  }

  queueResolveResponse(resolved: ResolvedElement): this {
    this.resolveQueue.push(resolved);
    return this;
  }

  queueTapResponse(result: ActionResult): this {
    this.tapResultQueue.push(result);
    return this;
  }

  queueInputResponse(result: ActionResult): this {
    this.inputResultQueue.push(result);
    return this;
  }

  // ── DeviceController implementation ────────────────────────────

  async getUiTree(): Promise<RawElement> {
    this.calls.push({ method: "getUiTree", args: undefined });
    return this.treeQueue.shift() ?? { elementId: "el_root", children: [] };
  }

  async getUiSummary(): Promise<CompactElement[]> {
    this.calls.push({ method: "getUiSummary", args: undefined });
    return this.summaryQueue.shift() ?? [];
  }

  async resolveSelector(selector: Selector): Promise<ResolvedElement> {
    this.calls.push({ method: "resolveSelector", args: selector });
    return this.resolveQueue.shift() ?? { found: false };
  }

  async screenshot(): Promise<{ base64: string; format: "png" }> {
    this.calls.push({ method: "screenshot", args: undefined });
    return { base64: "iVBORw0KGgo=", format: "png" };
  }

  async getKeyboard(): Promise<KeyboardInfo> {
    this.calls.push({ method: "getKeyboard", args: undefined });
    return { visible: false, packageName: null, layout: "none", bounds: null, keys: [] };
  }

  async tap(selector: Selector): Promise<ActionResult> {
    this.calls.push({ method: "tap", args: selector });
    return this.tapResultQueue.shift() ?? { ok: true, reason: "mock" };
  }

  async tapCoordinates(x: number, y: number): Promise<void> {
    this.calls.push({ method: "tapCoordinates", args: { x, y } });
  }

  async longPressCoordinates(x: number, y: number, durationMs?: number): Promise<void> {
    this.calls.push({ method: "longPressCoordinates", args: { x, y, durationMs } });
  }

  async clearFocusedInput(): Promise<ActionResult> {
    this.calls.push({ method: "clearFocusedInput", args: {} });
    return { ok: true, reason: "mock" };
  }

  async swipe(fromX: number, fromY: number, toX: number, toY: number, durationMs?: number): Promise<void> {
    this.calls.push({ method: "swipe", args: { fromX, fromY, toX, toY, durationMs } });
  }

  async inputText(selector: Selector, text: string): Promise<ActionResult> {
    this.calls.push({ method: "inputText", args: { selector, text } });
    return this.inputResultQueue.shift() ?? { ok: true, reason: "mock" };
  }

  async typeViaKeyboard(text: string, perKeyDelayMs?: number, clearFirst?: boolean): Promise<TypeKeyboardResult> {
    this.calls.push({ method: "typeViaKeyboard", args: { text, perKeyDelayMs, clearFirst } });
    return { success: true, typed: text.length, total: text.length, reason: "mock" };
  }

  async pressKey(key: "back" | "home" | "enter"): Promise<void> {
    this.calls.push({ method: "pressKey", args: { key } });
  }

  async listApps(): Promise<{ appId: string; label?: string }[]> {
    this.calls.push({ method: "listApps", args: undefined });
    return [];
  }

  async launchApp(appId: string): Promise<void> {
    this.calls.push({ method: "launchApp", args: { appId } });
  }

  async forceStopApp(appId: string): Promise<void> {
    this.calls.push({ method: "forceStopApp", args: { appId } });
  }

  async currentForeground(): Promise<{ appId: string; screen?: string }> {
    this.calls.push({ method: "currentForeground", args: undefined });
    return { appId: "mock.app", screen: "MockScreen" };
  }

  async dispose(): Promise<void> {
    this.calls.push({ method: "dispose", args: undefined });
  }
}
