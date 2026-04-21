import type { ScriptSelector } from "./script-selector.js";

/**
 * Tagged union of every step a YAML test script can contain.
 * Each variant is keyed by `command` — the runner dispatches
 * to the corresponding `CommandDefinition` at execution time.
 *
 * Adding a new step type = extending this union + creating
 * the matching `CommandDefinition` file. The runner itself
 * does not change.
 */
export type ScriptStep =
  | LaunchAppStep
  | TapStep
  | TypeStep
  | WaitForStep
  | AssertVisibleStep
  | AssertNotVisibleStep
  | ScreenshotStep
  | SwipeStep
  | PressKeyStep
  | BackStep
  | SleepStep
  | CaptureStep
  | AssertApiStep
  | ExtractStep
  | HandleStep
  | BranchStep
  | RunFlowStep
  | PointerStep;

export interface LaunchAppStep {
  readonly command: "launchApp";
}

export interface TapStep {
  readonly command: "tap";
  readonly selector: ScriptSelector;
}

export interface TypeStep {
  readonly command: "type";
  readonly text: string;
  readonly into?: ScriptSelector;
}

export interface WaitForStep {
  readonly command: "waitFor";
  readonly selector: ScriptSelector;
  readonly timeoutMs?: number;
}

export interface AssertVisibleStep {
  readonly command: "assertVisible";
  readonly selector: ScriptSelector;
  /** Polling timeout in ms. Default: instant check (no polling). */
  readonly timeoutMs?: number;
}

export interface AssertNotVisibleStep {
  readonly command: "assertNotVisible";
  readonly selector: ScriptSelector;
  /** Polling timeout in ms. Default: instant check (no polling). */
  readonly timeoutMs?: number;
}

export interface ScreenshotStep {
  readonly command: "screenshot";
  readonly label?: string;
}

export interface SwipeStep {
  readonly command: "swipe";
  readonly direction: "up" | "down" | "left" | "right";
}

export interface PressKeyStep {
  readonly command: "pressKey";
  readonly key: string;
}

export interface BackStep {
  readonly command: "back";
}

export interface SleepStep {
  readonly command: "sleep";
  readonly ms: number;
}

export interface CaptureStep {
  readonly command: "capture";
  /** URL pattern to match, e.g. "POST /api/transfer". */
  readonly pattern: string;
  /** Variable name to store the captured request under. */
  readonly as: string;
}

export interface AssertApiStep {
  readonly command: "assertApi";
  /** Variable name referencing a previously captured request. */
  readonly from: string;
  /** Expected HTTP status code. */
  readonly status?: number;
  /** Dot-path body assertions, e.g. { "$.status": "completed" }. */
  readonly body?: Readonly<Record<string, unknown>>;
}

/**
 * Extract values from a captured API response into runtime
 * variables. Extracted values are available via `${name}` in
 * subsequent steps.
 *
 * ```yaml
 * - extract:
 *     from: login
 *     values:
 *       token: $.body.token
 *       userId: $.body.user.id
 * ```
 */
export interface ExtractStep {
  readonly command: "extract";
  /** Variable name of a previously captured request. */
  readonly from: string;
  /** Map of variable name → dot-path to extract. */
  readonly values: Readonly<Record<string, string>>;
}

/**
 * UI-based branching — detect which screen state the app is
 * in and execute the matching `do` block. First match wins.
 *
 * ```yaml
 * - handle:
 *     - when: { visible: "Enter OTP" }
 *       do:
 *         - type: "123456"
 *         - tap: "Verify"
 *     - when: { visible: "Success" }
 *       do:
 *         - screenshot: success
 *     - otherwise: fail
 * ```
 */
export interface HandleStep {
  readonly command: "handle";
  readonly branches: readonly HandleBranch[];
  /** Action when no branch matches: "fail" (default) or "skip". */
  readonly otherwise?: "fail" | "skip";
  /**
   * Maximum time the runner waits for one of the branches' `when`
   * conditions to hold, in milliseconds. When the timeout expires
   * without any branch matching, the `otherwise` action fires.
   * Default chosen to cover typical screen transitions so simple
   * scripts need no explicit `sleep:` before a `handle`.
   */
  readonly timeout?: number;
}

export interface HandleBranch {
  readonly when: HandleCondition;
  /** Inline steps or file path to a flow fragment. */
  readonly do: readonly ScriptStep[] | string;
}

export interface HandleCondition {
  /** Element must be visible on screen. */
  readonly visible?: string | ScriptSelector;
  /** Element must NOT be visible. */
  readonly notVisible?: string | ScriptSelector;
}

/**
 * API-based branching — route based on a captured API response.
 * First matching `on` entry executes its `do` block.
 *
 * ```yaml
 * - branch:
 *     from: payment
 *     on:
 *       - match: { body: { $.requires_otp: true } }
 *         do:
 *           - waitFor: "Enter OTP"
 *       - match: { status: 400 }
 *         do:
 *           - screenshot: error
 *     default:
 *       - waitFor: "Success"
 * ```
 */
export interface BranchStep {
  readonly command: "branch";
  /** Variable name of a previously captured request. */
  readonly from: string;
  readonly on: readonly BranchCase[];
  /** Steps or file to run if no case matches. */
  readonly default?: readonly ScriptStep[] | string;
}

export interface BranchCase {
  readonly match: BranchMatchCondition;
  /** Inline steps or file path to a flow fragment. */
  readonly do: readonly ScriptStep[] | string;
}

export interface BranchMatchCondition {
  /** Match HTTP status code. */
  readonly status?: number;
  /** Match body fields via dot-path. */
  readonly body?: Readonly<Record<string, unknown>>;
}

/**
 * Execute another YML script file as a sub-flow.
 *
 * ```yaml
 * - runFlow: flows/login.yml
 * - runFlow:
 *     file: flows/login.yml
 *     env:
 *       email: other@test.com
 * ```
 */
export interface RunFlowStep {
  readonly command: "runFlow";
  /** Path to the sub-flow YML file (relative to current script). */
  readonly file: string;
  /** Extra env variables to pass to the sub-flow. */
  readonly env?: Readonly<Record<string, string>>;
}

/**
 * Target of a `down` or `move` pointer action. Either a selector
 * (resolved at dispatch / at the action's tick — see the pointer
 * proposal for the hybrid rule) OR absolute screen coordinates
 * in points.
 */
export type PointerTarget =
  | { readonly selector: ScriptSelector }
  | { readonly point: { readonly x: number; readonly y: number } };

/**
 * One primitive in a pointer sequence. W3C Actions semantics:
 *
 *   - `down`: pointer touches down at the resolved point.
 *   - `move`: drag to a new resolved point.
 *   - `wait`: hold the current position for `ms` milliseconds.
 *   - `up`: release the pointer.
 *
 * `down` and `move` optionally carry `pressure` (0.0–1.0) for
 * pressure-sensitive input (iOS 3D Touch, Android API 26+
 * `willContinue` strokes). The compiler propagates this onto
 * the generated `Waypoint.pressure`; drivers without the
 * capability reject scripts that set it via the
 * `canPressure` capability gate.
 *
 * A pointer sequence must open with `down` and close with `up`.
 * The runner validates shape; see `pointer.command.ts`.
 */
export type PointerAction =
  | { readonly type: "down"; readonly target: PointerTarget; readonly pressure?: number }
  | { readonly type: "move"; readonly target: PointerTarget; readonly pressure?: number }
  | { readonly type: "wait"; readonly ms: number }
  | { readonly type: "up" };

/**
 * One pointer's time-ordered action list, used in the
 * multi-pointer form (`pointer.pointers`). See pointer.command.ts
 * for validation rules.
 */
export interface PointerGroup {
  readonly id: string;
  readonly actions: readonly PointerAction[];
}

/**
 * General-purpose pointer gesture. Expresses every W3C-Actions-
 * compatible sequence: tap, long-press, drag, press-and-drag,
 * flick, pinch, rotate, multi-finger.
 *
 * Two mutually exclusive forms:
 *
 *   - `actions` (single pointer): one sequence the active
 *     pointer walks through.
 *   - `pointers` (multi-pointer): parallel sequences keyed by
 *     author-chosen id. Aligns on a shared wall clock anchored
 *     at the first `down`. Requires `canMultiPointer` on the
 *     active driver.
 *
 * ```yaml
 * - pointer:
 *     actions:
 *       - down: "Item A"
 *       - wait: 800
 *       - move: { x: 300, y: 600 }
 *       - up
 *     moveDurationMs: 300
 * ```
 *
 * Exactly one of `actions` or `pointers` must be present. See
 * `docs/pointer.md` for the end-user reference and
 * `docs/yml-script-reference.md` §pointer for the schema.
 */
export interface PointerStep {
  readonly command: "pointer";
  /** Single-pointer form. Mutually exclusive with `pointers`. */
  readonly actions?: readonly PointerAction[];
  /** Multi-pointer form. Mutually exclusive with `actions`. */
  readonly pointers?: readonly PointerGroup[];
  /**
   * Duration in ms for each `move` action within this gesture.
   * Defaults to 200 ms when omitted.
   */
  readonly moveDurationMs?: number;
}
