import { useEffect, useMemo, useRef, useState } from "react";
import { getFeature } from "../../../state/core/registry.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";
import type { WorkspaceStateApi } from "../../../state/features/workspace-state/index.js";
import { WORKSPACE_STATE_KEY } from "../../../state/features/workspace-state/index.js";

/**
 * Quick jump through files the user has touched recently in the
 * active workspace. Opens on ⌘E (action `file.openRecent`).
 * Enter or click on a row opens that file.
 */
export interface RecentFilesPopupProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

export function RecentFilesPopup({ open, onClose }: RecentFilesPopupProps) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const items = getFeature<WorkspaceStateApi>(WORKSPACE_STATE_KEY).getSnapshot().state.recentFiles;
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (q.length === 0) return items;
    return items.filter((p) => p.toLowerCase().includes(q));
  }, [items, query]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setSelected(0);
      inputRef.current?.focus();
    }
  }, [open]);

  if (!open) return null;

  const pick = (path: string) => {
    void getFeature<EditorApi>(EDITOR_KEY).openFile(path);
    onClose();
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelected((s) => Math.min(s + 1, filtered.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelected((s) => Math.max(s - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const path = filtered[selected];
      if (path) pick(path);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-28"
      style={{ background: "rgba(0,0,0,0.45)" }}
      onClick={onClose}
    >
      <div
        className="w-[540px] rounded-md shadow-xl overflow-hidden"
        style={{
          background: "var(--bg-1)",
          color: "var(--fg-0)",
          border: "1px solid var(--line)",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder="Recent files…"
          className="w-full px-3 py-3 text-sm outline-none"
          style={{
            background: "var(--bg-2)",
            color: "var(--fg-0)",
            borderBottom: "1px solid var(--line)",
          }}
        />
        <ul className="max-h-80 overflow-y-auto py-1">
          {filtered.length === 0 ? (
            <li
              className="px-3 py-2 text-xs"
              style={{ color: "var(--fg-2)" }}
            >
              {items.length === 0
                ? "No recent files in this workspace yet."
                : "No matches."}
            </li>
          ) : (
            filtered.map((path, i) => {
              const isSelected = i === selected;
              const name = path.split("/").pop() ?? path;
              return (
                <li
                  key={path}
                  className="flex flex-col px-3 py-1.5 text-sm cursor-pointer"
                  style={{
                    background: isSelected
                      ? "var(--bg-hover)"
                      : "transparent",
                  }}
                  onMouseEnter={() => setSelected(i)}
                  onClick={() => pick(path)}
                >
                  <span>{name}</span>
                  <span
                    className="text-xs truncate"
                    style={{ color: "var(--fg-2)" }}
                  >
                    {path}
                  </span>
                </li>
              );
            })
          )}
        </ul>
      </div>
    </div>
  );
}
