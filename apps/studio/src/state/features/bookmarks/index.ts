import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  Bookmark,
  BookmarksApi,
  BookmarksSnapshot,
} from "./bookmarks.contract.js";
import {
  createZustandBookmarks,
  type BookmarksDeps,
} from "./bookmarks.zustand.js";

export type { Bookmark, BookmarksApi, BookmarksSnapshot };

export const BOOKMARKS_KEY = "bookmarks";

export function createBookmarks(deps: BookmarksDeps): BookmarksApi {
  return createZustandBookmarks(deps);
}

export function useBookmarks(): BookmarksSnapshot &
  Pick<BookmarksApi, "toggle" | "remove" | "clear" | "isBookmarked"> {
  const api = getFeature<BookmarksApi>(BOOKMARKS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    toggle: api.toggle,
    remove: api.remove,
    clear: api.clear,
    isBookmarked: api.isBookmarked,
  };
}
