/**
 * Shared types crossing the MCP port. Mirror the shapes the 27-tool
 * MCP surface exposes but stay deliberately platform-agnostic:
 * callers don't know whether they're talking to an embedded stdio
 * process or a remote endpoint.
 */

export type StepTokenKind =
  | "keyword"
  | "identifier"
  | "string"
  | "punct"
  | "mask";

export interface StepToken {
  readonly kind: StepTokenKind;
  readonly text: string;
}

export type Platform = "ios" | "android";

export type DeviceKind = "simulator" | "emulator" | "device";

export interface Device {
  readonly id: string;
  readonly platform: Platform;
  readonly kind: DeviceKind;
  readonly name: string;
  readonly model?: string;
  readonly osVersion?: string;
}

export interface App {
  readonly id: string;
  readonly name: string;
}

export interface RunOpts {
  readonly deviceId: string;
  /** Extra variables merged over the script's own `env` block. */
  readonly env?: Readonly<Record<string, string>>;
  /**
   * Absolute directory the runtime should resolve relative paths
   * against (scenarios reference child scripts; scripts reference
   * `requires` and `runFlow` paths). Required for scenario runs;
   * optional for plain scripts that only use absolute paths.
   */
  readonly cwd?: string;
}

/**
 * Canonical UI element snapshot returned by `StudioRuntime.getUiTree`.
 * Mirrors the driver-core `TreeNode` shape but stays owned by the
 * Studio domain so alternative runtime adapters (remote, mock) can
 * satisfy it without importing driver internals.
 *
 * `attributes` holds platform-neutral keys (`id`, `text`, `label`,
 * `class`, `bounds` — see driver-core's `AttrKeys` for the full set).
 * State booleans sit at the top level because they are the hot-path
 * filter predicates.
 */
export interface UiTreeNode {
  readonly attributes: Readonly<Record<string, string>>;
  readonly children: readonly UiTreeNode[];
  readonly clickable?: boolean;
  readonly enabled?: boolean;
  readonly focused?: boolean;
  readonly selected?: boolean;
  readonly checked?: boolean;
  readonly visible?: boolean;
}

export type RunEvent =
  | { readonly type: "runStarted"; readonly runId: string; readonly startedAt: number }
  | {
      readonly type: "stepStarted";
      readonly stepIndex: number;
      readonly command: string;
      readonly summary: string;
      readonly tokens: readonly StepToken[];
      readonly depth: number;
      readonly line?: number;
    }
  | {
      readonly type: "stepCompleted";
      readonly stepIndex: number;
      readonly command: string;
      readonly ok: boolean;
      readonly durationMs: number;
      readonly summary: string;
      readonly tokens: readonly StepToken[];
      readonly detail?: string;
      readonly depth: number;
      readonly line?: number;
    }
  | {
      readonly type: "screenshot";
      readonly stepIndex: number;
      readonly bytes: Uint8Array;
      readonly label?: string;
    }
  | {
      readonly type: "consoleLog";
      readonly line: string;
      readonly level: "info" | "warn" | "error";
      readonly at: number;
    }
  | {
      readonly type: "runCompleted";
      readonly ok: boolean;
      readonly completedAt: number;
      readonly failedAtStep?: number;
    }
  | {
      readonly type: "scenarioStarted";
      readonly scenarioName: string;
      readonly totalScripts: number;
    }
  | {
      readonly type: "scriptStarted";
      readonly scriptIndex: number;
      readonly scriptPath: string;
    }
  | {
      readonly type: "scriptCompleted";
      readonly scriptIndex: number;
      readonly scriptPath: string;
      readonly status: "passed" | "failed" | "skipped" | "errored";
      readonly durationMs: number;
      readonly failedAtStep?: number;
    }
  | {
      readonly type: "scenarioCompleted";
      readonly ok: boolean;
      readonly totalScripts: number;
      readonly passedScripts: number;
      readonly durationMs: number;
    };
