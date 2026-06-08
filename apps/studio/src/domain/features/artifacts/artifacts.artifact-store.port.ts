import type { RunEvent } from "../runtime/index.js";
import type { RunMetadata, StoredArtifact } from "./artifacts.types.js";

/**
 * Studio's artifact persistence contract. Implementations decide
 * where bytes land — the default adapter writes to the OS
 * app-data dir.
 *
 * Every run owns an immutable-ish folder: metadata is written
 * once at start, the event log is append-only, artifacts are
 * saved by filename, metadata is patched at run completion. One
 * writer per run — no concurrent access required.
 */
export interface ArtifactStore {
  createRun(meta: RunMetadata): Promise<void>;
  appendEvent(runId: string, event: RunEvent): Promise<void>;
  saveArtifact(
    runId: string,
    name: string,
    bytes: Uint8Array,
    extras?: Partial<Omit<StoredArtifact, "name" | "size">>,
  ): Promise<StoredArtifact>;
  finalizeRun(runId: string, patch: Partial<RunMetadata>): Promise<void>;

  listRuns(): Promise<readonly RunMetadata[]>;
  getRun(runId: string): Promise<RunMetadata | null>;
  getEvents(runId: string): AsyncIterable<RunEvent>;
  listArtifacts(runId: string): Promise<readonly StoredArtifact[]>;
  readArtifact(runId: string, name: string): Promise<Uint8Array>;
  deleteRun(runId: string): Promise<void>;
}
