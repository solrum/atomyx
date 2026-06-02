import type { RunEvent } from "../runtime/index.js";
import type { ArtifactStore } from "./artifact-store.port.js";
import type { RunMetadata, StoredArtifact } from "./types.js";

interface RunEntry {
  meta: RunMetadata;
  events: RunEvent[];
  artifacts: Map<string, { meta: StoredArtifact; bytes: Uint8Array }>;
}

/**
 * In-memory artifact store. Used by UI and state tests so they can
 * assert on persisted artefacts without touching the filesystem.
 */
export class MockArtifactStore implements ArtifactStore {
  private readonly runs = new Map<string, RunEntry>();

  async createRun(meta: RunMetadata): Promise<void> {
    this.runs.set(meta.runId, {
      meta,
      events: [],
      artifacts: new Map(),
    });
  }

  async appendEvent(runId: string, event: RunEvent): Promise<void> {
    this.require(runId).events.push(event);
  }

  async saveArtifact(
    runId: string,
    name: string,
    bytes: Uint8Array,
    extras: Partial<Omit<StoredArtifact, "name" | "size">> = {},
  ): Promise<StoredArtifact> {
    const entry = this.require(runId);
    const meta: StoredArtifact = {
      name,
      size: bytes.byteLength,
      ...extras,
    };
    entry.artifacts.set(name, { meta, bytes });
    return meta;
  }

  async finalizeRun(runId: string, patch: Partial<RunMetadata>): Promise<void> {
    const entry = this.require(runId);
    entry.meta = { ...entry.meta, ...patch };
  }

  async listRuns(): Promise<readonly RunMetadata[]> {
    return Array.from(this.runs.values()).map((r) => r.meta);
  }

  async getRun(runId: string): Promise<RunMetadata | null> {
    return this.runs.get(runId)?.meta ?? null;
  }

  async *getEvents(runId: string): AsyncIterable<RunEvent> {
    const entry = this.require(runId);
    for (const event of entry.events) yield event;
  }

  async listArtifacts(runId: string): Promise<readonly StoredArtifact[]> {
    return Array.from(this.require(runId).artifacts.values()).map((a) => a.meta);
  }

  async readArtifact(runId: string, name: string): Promise<Uint8Array> {
    const entry = this.require(runId);
    const item = entry.artifacts.get(name);
    if (!item) {
      throw new Error(`MockArtifactStore: artifact "${name}" not found in run ${runId}`);
    }
    return item.bytes;
  }

  async deleteRun(runId: string): Promise<void> {
    this.runs.delete(runId);
  }

  private require(runId: string): RunEntry {
    const entry = this.runs.get(runId);
    if (!entry) {
      throw new Error(`MockArtifactStore: run "${runId}" not found`);
    }
    return entry;
  }
}
