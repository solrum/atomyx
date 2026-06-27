import type { ActionDefinition } from "../../../domain/features/actions/index.js";

export type { ActionDefinition };

export type ActionHandler = () => Promise<void> | void;

export interface ActionsSnapshot {
  readonly definitions: readonly ActionDefinition[];
  readonly paletteOpen: boolean;
  readonly paletteQuery: string;
}

export interface ActionsApi {
  getSnapshot(): ActionsSnapshot;
  subscribe(listener: () => void): () => void;
  registerHandler(id: string, handler: ActionHandler): () => void;
  openPalette(): void;
  closePalette(): void;
  setQuery(query: string): void;
  execute(id: string): Promise<void>;
}
