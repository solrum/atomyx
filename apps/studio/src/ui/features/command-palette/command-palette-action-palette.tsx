import { useEffect, useMemo, useRef, useState } from "react";
import type { ActionDefinition } from "../../../domain/features/actions/index.js";
import { useActions } from "../../../state/features/actions/index.js";

/**
 * IntelliJ-style "Find Action" palette. Opens on ⌘⇧A (wired via
 * the global keymap), fuzzy-filters actions by label + category,
 * and executes the selected item on Enter.
 *
 * Fuzzy matcher is simple — substring match on a normalized
 * label, ranked by match position. Replace with fzf if we grow
 * past ~50 actions.
 */
export function ActionPalette() {
  const { paletteOpen, paletteQuery, definitions, setQuery, execute, closePalette } = useActions();
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = useMemo(
    () => filterActions(definitions, paletteQuery),
    [definitions, paletteQuery],
  );

  useEffect(() => {
    setSelected(0);
  }, [paletteQuery, paletteOpen]);

  useEffect(() => {
    if (paletteOpen) {
      inputRef.current?.focus();
    }
  }, [paletteOpen]);

  if (!paletteOpen) return null;

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      closePalette();
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setSelected((s) => Math.min(s + 1, filtered.length - 1));
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setSelected((s) => Math.max(s - 1, 0));
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      const pick = filtered[selected];
      if (pick) void execute(pick.id);
      return;
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-28"
      style={{ background: "rgba(0,0,0,0.45)" }}
      onClick={closePalette}
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
          value={paletteQuery}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder="Find Action…"
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
              No matching actions.
            </li>
          ) : (
            filtered.map((action, i) => {
              const isSelected = i === selected;
              return (
                <li
                  key={action.id}
                  className="flex items-center justify-between px-3 py-1.5 text-sm cursor-pointer"
                  style={{
                    background: isSelected
                      ? "var(--bg-hover)"
                      : "transparent",
                  }}
                  onMouseEnter={() => setSelected(i)}
                  onClick={() => void execute(action.id)}
                >
                  <div>
                    <span>{action.label}</span>
                    <span
                      className="ml-2 text-xs"
                      style={{ color: "var(--fg-2)" }}
                    >
                      {action.category}
                    </span>
                  </div>
                  {action.shortcut ? (
                    <span
                      className="text-xs font-mono"
                      style={{ color: "var(--fg-2)" }}
                    >
                      {action.shortcut}
                    </span>
                  ) : null}
                </li>
              );
            })
          )}
        </ul>
      </div>
    </div>
  );
}

function filterActions(
  definitions: readonly ActionDefinition[],
  query: string,
): readonly ActionDefinition[] {
  const q = query.trim().toLowerCase();
  if (q.length === 0) return definitions;
  const scored: Array<{ action: ActionDefinition; score: number }> = [];
  for (const action of definitions) {
    const haystack = `${action.label} ${action.category}`.toLowerCase();
    const idx = haystack.indexOf(q);
    if (idx === -1) continue;
    scored.push({ action, score: idx });
  }
  scored.sort((a, b) => a.score - b.score);
  return scored.map((s) => s.action);
}
