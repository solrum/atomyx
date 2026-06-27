import type { RunConfig } from "../../../domain/features/run-configs/index.js";

export type { RunConfig };

export interface RunConfigsSnapshot {
  readonly workspacePath: string | null;
  readonly configs: readonly RunConfig[];
  readonly activeId: string | null;
}

export interface RunConfigsApi {
  getSnapshot(): RunConfigsSnapshot;
  subscribe(listener: () => void): () => void;
  hydrate(workspacePath: string): Promise<void>;
  setActive(id: string | null): void;
  save(
    patch: Partial<RunConfig> & { readonly id?: string; readonly name: string },
  ): Promise<RunConfig>;
  remove(id: string): Promise<void>;
  duplicate(id: string): Promise<RunConfig | null>;
}
