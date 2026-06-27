import type { TodoHit } from "../../../domain/features/todos/index.js";

export interface TodosSnapshot {
  readonly items: readonly TodoHit[];
  readonly loading: boolean;
}

export interface TodosApi {
  getSnapshot(): TodosSnapshot;
  subscribe(listener: () => void): () => void;
  refresh(): Promise<void>;
  clear(): void;
}

export type { TodoHit };
