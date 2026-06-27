import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";

export interface ContextMenuItem {
  readonly id: string;
  readonly label: string;
  readonly shortcut?: string;
  readonly disabled?: boolean;
  readonly danger?: boolean;
  readonly onSelect: () => void;
}

export type ContextMenuEntry = ContextMenuItem | "separator";

export interface ContextMenuProps {
  readonly children: (props: {
    readonly onContextMenu: (e: React.MouseEvent) => void;
  }) => ReactNode;
  readonly items: readonly ContextMenuEntry[];
}

/**
 * Minimal right-click context menu. The child-render-prop pattern
 * lets the wrapped element keep its own props free — menus attach
 * via `onContextMenu` only. Click outside / Esc dismisses; arrow
 * keys + Enter select entries.
 */
export function ContextMenu({ children, items }: ContextMenuProps) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ x: number; y: number }>({ x: 0, y: 0 });
  const [cursor, setCursor] = useState(0);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        setOpen(false);
      } else if (e.key === "ArrowDown") {
        e.preventDefault();
        setCursor((c) => nextSelectable(items, c, 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setCursor((c) => nextSelectable(items, c, -1));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const entry = items[cursor];
        if (entry && entry !== "separator" && !entry.disabled) {
          entry.onSelect();
          setOpen(false);
        }
      }
    };
    window.addEventListener("mousedown", onClick);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onClick);
      window.removeEventListener("keydown", onKey);
    };
  }, [open, cursor, items]);

  const onContextMenu = (e: React.MouseEvent) => {
    e.preventDefault();
    setPos({ x: e.clientX, y: e.clientY });
    setCursor(firstSelectable(items));
    setOpen(true);
  };

  return (
    <>
      {children({ onContextMenu })}
      {open ? (
        <div
          ref={menuRef}
          className="fixed z-50 min-w-[220px] rounded-md shadow-xl py-1 text-[13px]"
          style={{
            top: pos.y,
            left: pos.x,
            background: "var(--bg-2)",
            color: "var(--fg-0)",
            border: "1px solid var(--line)",
            fontFamily:
              "ui-sans-serif, system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
          }}
          role="menu"
        >
          {items.map((entry, i) =>
            entry === "separator" ? (
              <div
                key={`sep-${i}`}
                className="my-1 h-px"
                style={{ background: "var(--line)" }}
              />
            ) : (
              <button
                type="button"
                key={entry.id}
                onClick={() => {
                  if (entry.disabled) return;
                  entry.onSelect();
                  setOpen(false);
                }}
                disabled={entry.disabled}
                onMouseEnter={() => setCursor(i)}
                className="w-full text-left flex items-center justify-between gap-3 px-3 py-1 disabled:opacity-40 whitespace-nowrap"
                style={{
                  background:
                    i === cursor ? "var(--bg-hover)" : "transparent",
                  color: entry.danger
                    ? "var(--err)"
                    : "var(--fg-0)",
                }}
                role="menuitem"
              >
                <span>{entry.label}</span>
                {entry.shortcut ? (
                  <span
                    className="text-xs font-mono"
                    style={{ color: "var(--fg-2)" }}
                  >
                    {entry.shortcut}
                  </span>
                ) : null}
              </button>
            ),
          )}
        </div>
      ) : null}
    </>
  );
}

function firstSelectable(entries: readonly ContextMenuEntry[]): number {
  for (let i = 0; i < entries.length; i++) {
    const e = entries[i];
    if (e !== "separator" && !(e as ContextMenuItem).disabled) return i;
  }
  return 0;
}

function nextSelectable(
  entries: readonly ContextMenuEntry[],
  from: number,
  step: 1 | -1,
): number {
  const n = entries.length;
  if (n === 0) return 0;
  let i = from;
  for (let tries = 0; tries < n; tries++) {
    i = (i + step + n) % n;
    const e = entries[i];
    if (e !== "separator" && !(e as ContextMenuItem).disabled) return i;
  }
  return from;
}
