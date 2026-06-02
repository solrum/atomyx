import type {
  RunMetadata,
  RunResult,
} from "../../../domain/features/artifacts/index.js";
import type {
  RunEvent,
  RunOpts,
} from "../../../domain/features/runtime/index.js";

export type { RunMetadata, RunResult, RunEvent, RunOpts };

export type ScenarioScriptStatus =
  | "queued"
  | "running"
  | "passed"
  | "failed"
  | "skipped"
  | "errored";

export interface ScenarioScriptRow {
  readonly index: number;
  readonly path: string;
  readonly status: ScenarioScriptStatus;
  readonly durationMs?: number;
  readonly failedAtStep?: number;
}

export interface ScenarioLiveState {
  readonly name: string;
  readonly totalScripts: number;
  readonly currentIndex: number;
  readonly scripts: readonly ScenarioScriptRow[];
}

export interface LiveRun {
  readonly runId: string;
  readonly scriptPath: string;
  readonly deviceId: string;
  readonly startedAt: number;
  readonly events: readonly RunEvent[];
  readonly result: RunResult | "running";
  /**
   * Populated only when the active document is a scenario; tracks
   * per-script progress alongside the flat `events` list. Absent
   * for plain script runs.
   */
  readonly scenario?: ScenarioLiveState;
}

export interface RunsSnapshot {
  readonly live: LiveRun | null;
  readonly history: readonly RunMetadata[];
}

export interface RunsApi {
  getSnapshot(): RunsSnapshot;
  subscribe(listener: () => void): () => void;
  startRun(scriptPath: string, yaml: string, opts: RunOpts): Promise<void>;
  stopRun(): void;
  loadHistory(): Promise<void>;
  deleteRun(runId: string): Promise<void>;
}
