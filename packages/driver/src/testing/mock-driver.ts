import type {
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
} from "../driver/driver.port.js";
import type { TreeNode } from "../tree/tree-node.js";

/**
 * In-memory `Driver` implementation for unit tests.
 *
 * The mock is intentionally scripted rather than simulated:
 * tests stage a QUEUE of hierarchy snapshots, one per expected
 * `hierarchy()` call, and the mock returns them in order.
 * Gesture methods (`tap`, `swipe`, `longPress`, `pressKey`,
 * `inputText`) are RECORDED in the `calls` array so the test can
 * assert the right gestures were dispatched in the right order.
 *
 * Design decisions:
 *
 *   - Queue-based hierarchies lets tests model state changes
 *     between calls: "tree 1 is pre-scroll, tree 2 is after
 *     swipe, tree 3 is after second swipe". The mock does NOT
 *     try to actually mutate a tree — that's a simulation, which
 *     would be harder to get right than writing the expected
 *     tree out by hand.
 *
 *   - `calls` is a public mutable array. Tests push/pop/read
 *     freely. No assertion DSL inside the mock — callers use
 *     whatever framework they prefer (node:test + assert here).
 *
 *   - `onSwipe` hook lets tests react to swipes — for example,
 *     advance the `hierarchyQueue` to the "post-scroll" tree.
 *     This is how ScrollController tests verify that swipes
 *     actually move the element.
 *
 *   - All optional methods return safe defaults (`canX: false`
 *     for capabilities the test hasn't explicitly enabled) so
 *     tests only need to stub what they exercise.
 */
export class MockDriver implements Driver {
  readonly platform = "mock";
  capabilities: Capabilities = {
    canScreenshot: true,
    canEraseText: true,
    canWaitForIdle: false,
    canSetLocation: false,
    canSetOrientation: false,
    canHideKeyboard: true,
    canMultiPointer: false,
    canPressure: false,
    supportedKeyCodes: ["back", "home", "enter"],
  };

  private connected = false;

  /**
   * Queue of trees to return from successive `hierarchy()`
   * calls. `stageHierarchy()` pushes, `hierarchy()` shifts.
   * When the queue is empty, the last-staged tree is returned
   * repeatedly (sticky final state).
   */
  private hierarchyQueue: TreeNode[] = [];
  private lastHierarchy: TreeNode | null = null;

  /** Static screen size returned from `screenSize()`. */
  screen: Size = { width: 430, height: 932 };

  /** Recorded gesture + lifecycle calls in dispatch order. */
  public calls: MockCall[] = [];

  /** Invoked AFTER each swipe — tests use to advance state. */
  public onSwipe: ((from: Point, to: Point) => void) | null = null;

  // ── Test setup helpers ─────────────────────────────────────

  /**
   * Stage one tree to be returned from the next `hierarchy()`
   * call. Chain multiple `stageHierarchy` calls to model a
   * scripted sequence of states.
   */
  stageHierarchy(tree: TreeNode): this {
    this.hierarchyQueue.push(tree);
    return this;
  }

  /** Convenience: stage the same tree for N calls. */
  stageHierarchyRepeated(tree: TreeNode, times: number): this {
    for (let i = 0; i < times; i++) this.hierarchyQueue.push(tree);
    return this;
  }

  /** Reset recorded calls without touching staged hierarchies. */
  clearCalls(): void {
    this.calls = [];
  }

  // ── Driver interface ───────────────────────────────────────

  async connect(): Promise<void> {
    this.connected = true;
  }
  async disconnect(): Promise<void> {
    this.connected = false;
  }
  isConnected(): boolean {
    return this.connected;
  }

  async hierarchy(): Promise<TreeNode> {
    this.calls.push({ method: "hierarchy", args: [] });
    const tree = this.hierarchyQueue.shift() ?? this.lastHierarchy;
    if (!tree) {
      throw new Error(
        "MockDriver.hierarchy(): no tree staged. Call stageHierarchy() first.",
      );
    }
    this.lastHierarchy = tree;
    return tree;
  }

  async waitForIdle(timeoutMs: number): Promise<boolean> {
    this.calls.push({ method: "waitForIdle", args: [timeoutMs] });
    return true;
  }

  async tap(point: Point): Promise<void> {
    this.calls.push({ method: "tap", args: [point] });
  }

  async longPress(point: Point, durationMs: number): Promise<void> {
    this.calls.push({ method: "longPress", args: [point, durationMs] });
  }

  async swipe(from: Point, to: Point, durationMs: number): Promise<void> {
    this.calls.push({ method: "swipe", args: [from, to, durationMs] });
    if (this.onSwipe) this.onSwipe(from, to);
  }

  async dispatchGesture(gesture: Gesture): Promise<void> {
    this.calls.push({ method: "dispatchGesture", args: [gesture] });
  }

  async inputText(text: string): Promise<void> {
    this.calls.push({ method: "inputText", args: [text] });
  }

  async eraseText(count: number): Promise<void> {
    this.calls.push({ method: "eraseText", args: [count] });
  }

  async pressKey(key: KeyCode): Promise<KeyResult> {
    this.calls.push({ method: "pressKey", args: [key] });
    return { ok: true };
  }

  /**
   * Handler invoked on `hideKeyboard()`. Tests use this to
   * simulate the keyboard going away — e.g. by advancing the
   * hierarchy queue to a tree without a keyboard subtree.
   */
  public onHideKeyboard: (() => KeyResult | void) | null = null;

  setHideKeyboardHandler(fn: () => KeyResult | void): void {
    this.onHideKeyboard = fn;
  }

  async hideKeyboard(): Promise<KeyResult> {
    this.calls.push({ method: "hideKeyboard", args: [] });
    const result = this.onHideKeyboard?.();
    return result ?? { ok: true };
  }

  async launchApp(bundleId: string, args?: LaunchArgs): Promise<void> {
    this.calls.push({ method: "launchApp", args: [bundleId, args] });
  }

  async stopApp(bundleId: string): Promise<void> {
    this.calls.push({ method: "stopApp", args: [bundleId] });
  }

  async killApp(bundleId: string): Promise<void> {
    this.calls.push({ method: "killApp", args: [bundleId] });
  }

  async currentForeground(): Promise<ForegroundInfo> {
    return { bundleId: null };
  }

  async listApps(): Promise<readonly InstalledApp[]> {
    return [];
  }

  async screenshot(): Promise<Uint8Array> {
    return new Uint8Array();
  }

  async deviceInfo(): Promise<DeviceInfo> {
    return {
      platform: "mock",
      platformVersion: "0.0.0",
      model: "Mock",
      udid: "mock-udid",
      kind: "simulator",
    };
  }

  async screenSize(): Promise<Size> {
    return this.screen;
  }
}

export interface MockCall {
  readonly method: string;
  readonly args: readonly unknown[];
}
