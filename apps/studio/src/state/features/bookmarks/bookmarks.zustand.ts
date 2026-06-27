import { createStore } from "zustand/vanilla";
import {
  BOOKMARKS_CAP,
  type Bookmark,
} from "../../../domain/features/workspace-state/index.js";
import type { BookmarksApi, BookmarksSnapshot } from "./bookmarks.contract.js";

export interface BookmarksDeps {
  readonly getPersistedBookmarks: () => readonly Bookmark[] | null;
  readonly setPersistedBookmarks: (items: readonly Bookmark[]) => void;
  readonly subscribePersistence: (
    listener: (bookmarks: readonly Bookmark[] | null) => void,
  ) => () => void;
}

export function createZustandBookmarks(deps: BookmarksDeps): BookmarksApi {
  const store = createStore<BookmarksSnapshot>(() => ({ items: [] }));

  function persist(items: readonly Bookmark[]): void {
    const capped = items.slice(-BOOKMARKS_CAP);
    deps.setPersistedBookmarks(capped);
  }

  deps.subscribePersistence((bookmarks) => {
    if (bookmarks === null) return;
    if (bookmarks !== store.getState().items) {
      store.setState({ items: bookmarks });
    }
  });

  return {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    toggle(path, line, note) {
      const items = store.getState().items;
      const idx = items.findIndex((b) => b.path === path && b.line === line);
      if (idx >= 0) {
        const next = [...items.slice(0, idx), ...items.slice(idx + 1)];
        store.setState({ items: next });
        persist(next);
        return;
      }
      const next: readonly Bookmark[] = [
        ...items,
        { path, line, note, createdAt: Date.now() },
      ];
      store.setState({ items: next });
      persist(next);
    },

    remove(path, line) {
      const next = store
        .getState()
        .items.filter((b) => !(b.path === path && b.line === line));
      store.setState({ items: next });
      persist(next);
    },

    clear() {
      store.setState({ items: [] });
      persist([]);
    },

    isBookmarked(path, line) {
      return store.getState().items.some((b) => b.path === path && b.line === line);
    },
  };
}
