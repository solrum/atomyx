import { invoke } from "@tauri-apps/api/core";
import type { ArtifactStore } from "../../../domain/features/artifacts/index.js";
import type {
  RunMetadata,
  StoredArtifact,
} from "../../../domain/features/artifacts/index.js";
import type { RunEvent } from "../../../domain/features/runtime/index.js";

/**
 * Filesystem-backed artifact store. Delegates disk I/O to the Rust
 * backend so the renderer never touches paths directly —
 * permissions and retention policy live in one place.
 *
 * On-disk layout (`runs/<id>/meta.json`, `steps.jsonl`, artifact
 * file naming) is external-facing: bug-report zippers and
 * dashboards parse these files directly. Renaming fields or
 * reshaping the folder breaks every consumer.
 */
export class FsArtifactStore implements ArtifactStore {
  async createRun(meta: RunMetadata): Promise<void> {
    await invoke("artifacts_create_run", { meta });
  }

  async appendEvent(runId: string, event: RunEvent): Promise<void> {
    await invoke("artifacts_append_event", { runId, event });
  }

  async saveArtifact(
    runId: string,
    name: string,
    bytes: Uint8Array,
    extras: Partial<Omit<StoredArtifact, "name" | "size">> = {},
  ): Promise<StoredArtifact> {
    return invoke<StoredArtifact>("artifacts_save", {
      runId,
      name,
      bytes: Array.from(bytes),
      extras,
    });
  }

  async finalizeRun(runId: string, patch: Partial<RunMetadata>): Promise<void> {
    await invoke("artifacts_finalize_run", { runId, patch });
  }

  async listRuns(): Promise<readonly RunMetadata[]> {
    return invoke<readonly RunMetadata[]>("artifacts_list_runs");
  }

  async getRun(runId: string): Promise<RunMetadata | null> {
    return invoke<RunMetadata | null>("artifacts_get_run", { runId });
  }

  async *getEvents(runId: string): AsyncIterable<RunEvent> {
    const events = await invoke<readonly RunEvent[]>(
      "artifacts_get_events",
      { runId },
    );
    for (const event of events) yield event;
  }

  async listArtifacts(runId: string): Promise<readonly StoredArtifact[]> {
    return invoke<readonly StoredArtifact[]>("artifacts_list", { runId });
  }

  async readArtifact(runId: string, name: string): Promise<Uint8Array> {
    const bytes = await invoke<readonly number[]>("artifacts_read", {
      runId,
      name,
    });
    return Uint8Array.from(bytes);
  }

  async deleteRun(runId: string): Promise<void> {
    await invoke("artifacts_delete_run", { runId });
  }
}
