import { useEffect, useMemo, useRef, useState } from "react";
import { type NavLocation } from "../../../state/features/nav-history/index.js";
import type { NavHistoryApi } from "../../../state/features/nav-history/index.js";
import { NAV_HISTORY_KEY } from "../../../state/features/nav-history/index.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";
import { getFeature } from "../../../state/core/registry.js";
import { jumpActiveEditorTo } from "../editor/index.js";

export interface RecentLocationsProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

export function RecentLocations({ open, onClose }: RecentLocationsProps) {
  const [entries, setEntries] = useState<readonly NavLocation[]>([]);
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const [filter, setFilter] = useState("");

  useEffect(() => {
    if (!open) return;
    const initial = getFeature<NavHistoryApi>(NAV_HISTORY_KEY).getSnapshot().entries;
    setEntries([...initial].reverse());
    setSelected(0);
    setFilter("");
    inputRef.current?.focus();
  }, [open]);

  const visible = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (q.length === 0) return entries;
    return entries.filter((e) => e.path.toLowerCase().includes(q));
  }, [entries, filter]);

  if (!open) return null;

  const pick = async (loc: NavLocation) => {
    const navApi = getFeature<NavHistoryApi>(NAV_HISTORY_KEY);
    navApi.beginNavigation();
    try {
      await getFeature<EditorApi>(EDITOR_KEY).openFile(loc.path);
      requestAnimationFrame(() => {
        jumpActiveEditorTo(loc.line, loc.column);
        navApi.endNavigation();
      });
    } catch {
      navApi.endNavigation();
    }
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
      const loc = visible[selected];
      if (loc) void pick(loc);
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
          placeholder="Recent Locations — newest first"
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
              {entries.length === 0
                ? "No recent locations yet."
                : "No matches."}
            </div>
          ) : (
            visible.map((loc, i) => (
              <button
                key={`${loc.path}:${loc.line}:${loc.timestamp}`}
                type="button"
                onClick={() => void pick(loc)}
                onMouseEnter={() => setSelected(i)}
                className="w-full text-left px-3 py-1.5"
                style={{
                  background:
                    i === selected
                      ? "var(--bg-hover)"
                      : "transparent",
                }}
              >
                <div className="flex justify-between gap-2">
                  <span className="truncate">
                    {loc.path.split("/").pop()}
                  </span>
                  <span
                    className="text-xs"
                    style={{ color: "var(--fg-2)" }}
                  >
                    :{loc.line}
                  </span>
                </div>
                <div
                  className="text-xs truncate"
                  style={{ color: "var(--fg-2)" }}
                >
                  {loc.path}
                </div>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
