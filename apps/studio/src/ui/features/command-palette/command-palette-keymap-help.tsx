import { useEffect, useMemo, useRef, useState } from "react";
import { X } from "lucide-react";
import { ACTION_DEFINITIONS } from "../../../state/features/actions/index.js";
import type { ActionDefinition } from "../../../domain/features/actions/index.js";

export interface KeymapHelpProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

export function KeymapHelp({ open, onClose }: KeymapHelpProps) {
  const [filter, setFilter] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    setFilter("");
    inputRef.current?.focus();
  }, [open]);

  const grouped = useMemo(() => groupByCategory(ACTION_DEFINITIONS, filter), [filter]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-20"
      style={{ background: "rgba(0,0,0,0.45)" }}
      onClick={onClose}
    >
      <div
        className="w-[640px] max-h-[70vh] rounded-md shadow-xl flex flex-col overflow-hidden"
        style={{
          background: "var(--bg-1)",
          color: "var(--fg-0)",
          border: "1px solid var(--line)",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <header
          className="flex items-center justify-between px-3 py-2 border-b"
          style={{ borderColor: "var(--line)" }}
        >
          <span className="text-sm font-semibold">Keymap</span>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="opacity-60 hover:opacity-100"
          >
            <X className="h-4 w-4" />
          </button>
        </header>
        <input
          ref={inputRef}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter by action, category, or shortcut…"
          className="px-3 py-2 text-sm outline-none"
          style={{
            background: "var(--bg-2)",
            color: "var(--fg-0)",
            borderBottom: "1px solid var(--line)",
          }}
        />
        <div className="flex-1 min-h-0 overflow-y-auto py-1 text-sm">
          {grouped.length === 0 ? (
            <div
              className="px-3 py-2 text-xs"
              style={{ color: "var(--fg-2)" }}
            >
              No actions match &quot;{filter}&quot;.
            </div>
          ) : (
            grouped.map(([category, defs]) => (
              <section key={category} className="mb-2">
                <div
                  className="px-3 py-1 text-[11px] uppercase tracking-wider"
                  style={{ color: "var(--fg-2)" }}
                >
                  {category}
                </div>
                {defs.map((d) => (
                  <div
                    key={d.id}
                    className="px-3 py-1 flex items-center justify-between gap-2"
                  >
                    <span className="truncate">{d.label}</span>
                    {d.shortcut ? (
                      <span
                        className="text-xs font-mono flex-none"
                        style={{ color: "var(--fg-2)" }}
                      >
                        {d.shortcut}
                      </span>
                    ) : null}
                  </div>
                ))}
              </section>
            ))
          )}
        </div>
      </div>
    </div>
  );
}

function groupByCategory(
  defs: readonly ActionDefinition[],
  filter: string,
): readonly (readonly [string, readonly ActionDefinition[]])[] {
  const q = filter.trim().toLowerCase();
  const pool = q
    ? defs.filter(
        (d) =>
          d.label.toLowerCase().includes(q) ||
          d.category.toLowerCase().includes(q) ||
          (d.shortcut ?? "").toLowerCase().includes(q),
      )
    : defs;
  const by = new Map<string, ActionDefinition[]>();
  for (const d of pool) {
    const arr = by.get(d.category) ?? [];
    arr.push(d);
    by.set(d.category, arr);
  }
  return Array.from(by.entries()).sort(([a], [b]) => a.localeCompare(b));
}
