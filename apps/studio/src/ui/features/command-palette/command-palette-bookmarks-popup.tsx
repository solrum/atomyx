import { useEffect, useMemo, useRef, useState } from "react";
import { Bookmark as BookmarkIcon, Trash2 } from "lucide-react";
import { getFeature } from "../../../state/core/registry.js";
import {
  type Bookmark,
  type BookmarksApi,
  BOOKMARKS_KEY,
  useBookmarks,
} from "../../../state/features/bookmarks/index.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";
import { jumpActiveEditorTo } from "../editor/index.js";

export interface BookmarksPopupProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

export function BookmarksPopup({ open, onClose }: BookmarksPopupProps) {
  const { items: all } = useBookmarks();
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    setFilter("");
    setSelected(0);
    inputRef.current?.focus();
  }, [open]);

  const visible = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const base = [...all].reverse();
    if (q.length === 0) return base;
    return base.filter(
      (b) =>
        b.path.toLowerCase().includes(q) ||
        (b.note ?? "").toLowerCase().includes(q),
    );
  }, [all, filter]);

  if (!open) return null;

  const pick = async (b: Bookmark) => {
    await getFeature<EditorApi>(EDITOR_KEY).openFile(b.path);
    requestAnimationFrame(() => jumpActiveEditorTo(b.line, 1));
    onClose();
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelected((s) => Math.min(s + 1, visible.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelected((s) => Math.max(s - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const b = visible[selected];
      if (b) void pick(b);
    } else if (e.key === "Delete" || e.key === "Backspace") {
      if (e.metaKey || e.ctrlKey) {
        e.preventDefault();
        const b = visible[selected];
        if (b) getFeature<BookmarksApi>(BOOKMARKS_KEY).remove(b.path, b.line);
      }
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-24"
      style={{ background: "rgba(0,0,0,0.45)" }}
      onClick={onClose}
    >
      <div
        className="w-[620px] rounded-md shadow-xl overflow-hidden"
        style={{
          background: "var(--bg-1)",
          color: "var(--fg-0)",
          border: "1px solid var(--line)",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder="Bookmarks — ⌘⌫ to remove"
          className="w-full px-3 py-3 text-sm outline-none"
          style={{
            background: "var(--bg-2)",
            color: "var(--fg-0)",
            borderBottom: "1px solid var(--line)",
          }}
        />
        <div
          className="max-h-96 overflow-y-auto py-1 text-sm"
          style={{ color: "var(--fg-0)" }}
        >
          {visible.length === 0 ? (
            <div
              className="px-3 py-2 text-xs"
              style={{ color: "var(--fg-2)" }}
            >
              {all.length === 0
                ? "No bookmarks yet. ⌥⌘B toggles one at the cursor."
                : "No matches."}
            </div>
          ) : (
            visible.map((b, i) => (
              <div
                key={`${b.path}:${b.line}:${b.createdAt}`}
                className="w-full flex items-center gap-2 px-3 py-1.5"
                style={{
                  background:
                    i === selected
                      ? "var(--bg-hover)"
                      : "transparent",
                }}
                onMouseEnter={() => setSelected(i)}
              >
                <BookmarkIcon
                  className="h-3 w-3 flex-none"
                  style={{ color: "var(--accent)" }}
                />
                <button
                  type="button"
                  onClick={() => void pick(b)}
                  className="flex-1 min-w-0 text-left"
                >
                  <div className="flex justify-between gap-2">
                    <span className="truncate">{b.path.split("/").pop()}</span>
                    <span
                      className="text-xs"
                      style={{ color: "var(--fg-2)" }}
                    >
                      :{b.line}
                    </span>
                  </div>
                  <div
                    className="text-xs truncate"
                    style={{ color: "var(--fg-2)" }}
                  >
                    {b.note ?? b.path}
                  </div>
                </button>
                <button
                  type="button"
                  aria-label="Remove bookmark"
                  onClick={() =>
                    getFeature<BookmarksApi>(BOOKMARKS_KEY).remove(b.path, b.line)
                  }
                  className="opacity-50 hover:opacity-100 flex-none"
                >
                  <Trash2 className="h-3 w-3" />
                </button>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
