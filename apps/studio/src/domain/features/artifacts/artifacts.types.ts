/**
 * On-disk shape of a Studio run. The `runs/<runId>/` folder
 * layout is a public contract — external tools (bug-report
 * zippers, dashboards) parse these files directly. Renaming a
 * field, reshaping an event, or moving the folder breaks every
 * consumer downstream.
 */

export type RunResult = "passed" | "failed" | "cancelled";

export interface RunMetadata {
  readonly runId: string;
  readonly scriptPath: string;
  readonly scriptName: string;
  readonly deviceId: string;
  readonly startedAt: number;
  readonly completedAt?: number;
  readonly result?: RunResult;
  readonly failedAtStep?: number;
  readonly totalSteps?: number;
}

export interface StoredArtifact {
  readonly name: string;
  readonly size: number;
  readonly mimeType?: string;
  readonly stepIndex?: number;
}
