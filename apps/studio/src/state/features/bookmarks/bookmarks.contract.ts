import type { Bookmark } from "../../../domain/features/workspace-state/index.js";

export type { Bookmark };

export interface BookmarksSnapshot {
  readonly items: readonly Bookmark[];
}

export interface BookmarksApi {
  getSnapshot(): BookmarksSnapshot;
  subscribe(listener: () => void): () => void;
  toggle(path: string, line: number, note?: string): void;
  remove(path: string, line: number): void;
  clear(): void;
  isBookmarked(path: string, line: number): boolean;
}
