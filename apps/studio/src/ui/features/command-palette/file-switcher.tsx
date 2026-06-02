import { useEffect, useMemo, useRef, useState } from "react";
import {
  flattenFileTree,
  fuzzyScore,
  type FlatFileNode,
} from "../../../domain/features/workspace/index.js";
import { useWorkspace } from "../../../state/features/workspace/index.js";
import { getFeature } from "../../../state/core/registry.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";

export interface FileSwitcherProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

export function FileSwitcher({ open, onClose }: FileSwitcherProps) {
  const { tree } = useWorkspace();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setSelected(0);
    inputRef.current?.focus();
  }, [open]);

  const nodes = useMemo(() => flattenFileTree(tree), [tree]);

  const visible = useMemo(() => {
    const q = query.trim();
    if (q.length === 0) return nodes.slice(0, 50);
    return nodes
      .map((n) => ({ n, s: fuzzyScore(q, n.name) }))
      .filter((x) => x.s >= 0)
      .sort((a, b) => b.s - a.s)
      .slice(0, 50)
      .map((x) => x.n);
  }, [query, nodes]);

  if (!open) return null;

  const pick = async (node: FlatFileNode) => {
    await getFeature<EditorApi>(EDITOR_KEY).openFile(node.path);
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
      const n = visible[selected];
      if (n) void pick(n);
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
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setSelected(0);
          }}
          onKeyDown={onKeyDown}
          placeholder="Go to file…"
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
              {tree ? "No files match." : "No workspace open."}
            </div>
          ) : (
            visible.map((n, i) => (
              <button
                key={n.path}
                type="button"
                onClick={() => void pick(n)}
                onMouseEnter={() => setSelected(i)}
                className="w-full text-left px-3 py-1.5"
                style={{
                  background:
                    i === selected
                      ? "var(--bg-hover)"
                      : "transparent",
                }}
              >
                <div className="truncate">{n.name}</div>
                <div
                  className="text-xs truncate"
                  style={{ color: "var(--fg-2)" }}
                >
                  {n.parent}
                </div>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
