import { useEffect, useMemo, useRef, useState } from "react";
import { useEditor } from "../../../state/features/editor/index.js";
import { useActions } from "../../../state/features/actions/index.js";
import { useWorkspaceState } from "../../../state/features/workspace-state/index.js";
import { useWorkspace } from "../../../state/features/workspace/index.js";
import { useProjects } from "../../../state/features/projects/index.js";
import { useThemes } from "../../../state/features/theme/index.js";
import { collectFilePaths } from "../../../domain/features/workspace/index.js";

/**
 * Unified fuzzy search across files in the active workspace,
 * actions, themes, and recent projects. Opens on ⇧⇧
 * (double-shift) the IntelliJ way. Users skim categories; Enter
 * executes whatever row is selected.
 */
export interface FindEverywhereProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

type ResultKind = "file" | "action" | "theme" | "project";

interface Result {
  readonly kind: ResultKind;
  readonly label: string;
  readonly detail?: string;
  readonly invoke: () => void | Promise<void>;
}

export function FindEverywhere({ open, onClose }: FindEverywhereProps) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const editor = useEditor();
  const actions = useActions();
  const themes = useThemes();
  const projects = useProjects();
  const workspaceState = useWorkspaceState();
  const workspace = useWorkspace();

  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    const out: Result[] = [];

    // Files — walk the current workspace tree.
    for (const path of collectFilePaths(workspace.tree)) {
      if (q.length === 0 || path.toLowerCase().includes(q)) {
        out.push({
          kind: "file",
          label: path.split("/").pop() ?? path,
          detail: path,
          invoke: () => void editor.openFile(path),
        });
      }
    }

    // Actions — from the central registry.
    for (const action of actions.definitions) {
      const label = `${action.category}: ${action.label}`;
      if (q.length === 0 || label.toLowerCase().includes(q)) {
        out.push({
          kind: "action",
          label: action.label,
          detail: `${action.category}${action.shortcut ? " · " + action.shortcut : ""}`,
          invoke: () => void actions.execute(action.id),
        });
      }
    }

    // Themes — from the theme library.
    for (const theme of themes.available) {
      if (q.length === 0 || theme.label.toLowerCase().includes(q)) {
        out.push({
          kind: "theme",
          label: theme.label,
          detail: theme.source,
          invoke: () => void themes.setActiveId(theme.id),
        });
      }
    }

    // Recent projects — trigger workspace open when selected.
    for (const p of projects.items) {
      if (q.length === 0 || p.displayName.toLowerCase().includes(q)) {
        out.push({
          kind: "project",
          label: p.displayName,
          detail: p.path,
          invoke: () => void workspace.openFolder(p.path),
        });
      }
    }

    // Recent files — quicker to surface than deep-tree walk.
    for (const f of workspaceState.state.recentFiles) {
      if (q.length === 0 || f.toLowerCase().includes(q)) {
        out.push({
          kind: "file",
          label: f.split("/").pop() ?? f,
          detail: `recent — ${f}`,
          invoke: () => void editor.openFile(f),
        });
      }
    }

    // Rank: kind priority, then label match position.
    const priority: Record<ResultKind, number> = {
      file: 0,
      action: 1,
      theme: 2,
      project: 3,
    };
    return out
      .slice(0, 60)
      .sort((a, b) => priority[a.kind] - priority[b.kind]);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    query,
    open,
    workspace.tree,
    actions.definitions,
    themes.available,
    projects.items,
    workspaceState.state.recentFiles,
  ]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setSelected(0);
      inputRef.current?.focus();
    }
  }, [open]);

  if (!open) return null;

  const pick = (r: Result) => {
    void r.invoke();
    onClose();
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelected((s) => Math.min(s + 1, results.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelected((s) => Math.max(s - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const r = results[selected];
      if (r) pick(r);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-24"
      style={{ background: "rgba(0,0,0,0.45)" }}
      onClick={onClose}
    >
      <div
        className="w-[600px] rounded-md shadow-xl overflow-hidden"
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
          placeholder="Find everywhere — files, actions, themes, projects…"
          className="w-full px-3 py-3 text-sm outline-none"
          style={{
            background: "var(--bg-2)",
            color: "var(--fg-0)",
            borderBottom: "1px solid var(--line)",
          }}
        />
        <ul className="max-h-96 overflow-y-auto py-1">
          {results.length === 0 ? (
            <li
              className="px-3 py-2 text-xs"
              style={{ color: "var(--fg-2)" }}
            >
              No matches.
            </li>
          ) : (
            results.map((r, i) => {
              const isSelected = i === selected;
              return (
                <li
                  key={`${r.kind}:${r.label}:${i}`}
                  className="flex items-center justify-between gap-3 px-3 py-1.5 text-sm cursor-pointer"
                  style={{
                    background: isSelected
                      ? "var(--bg-hover)"
                      : "transparent",
                  }}
                  onMouseEnter={() => setSelected(i)}
                  onClick={() => pick(r)}
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate">{r.label}</div>
                    {r.detail ? (
                      <div
                        className="text-xs truncate"
                        style={{ color: "var(--fg-2)" }}
                      >
                        {r.detail}
                      </div>
                    ) : null}
                  </div>
                  <span
                    className="text-[11px] uppercase tracking-wider flex-shrink-0"
                    style={{ color: "var(--fg-2)" }}
                  >
                    {r.kind}
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

