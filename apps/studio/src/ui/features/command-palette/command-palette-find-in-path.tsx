import { useEffect, useRef, useState } from "react";
import { getFeature } from "../../../state/core/registry.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";
import type { WorkspaceApi } from "../../../state/features/workspace/index.js";
import { WORKSPACE_KEY } from "../../../state/features/workspace/index.js";
import type { WorkspaceSearchApi, WorkspaceSearchHit } from "../../../state/features/workspace-search/index.js";
import { WORKSPACE_SEARCH_KEY } from "../../../state/features/workspace-search/index.js";

type SearchHit = WorkspaceSearchHit;

/**
 * Find-in-path popup (⌘⇧F). Submits the query to the Rust
 * backend which scans every text file under the active
 * workspace and returns line-level hits. Debounce + cap keep
 * the typed-as-you-go experience snappy.
 */
export interface FindInPathProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

export function FindInPath({ open, onClose }: FindInPathProps) {
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<readonly SearchHit[]>([]);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<number | null>(null);

  useEffect(() => {
    if (open) {
      setQuery("");
      setHits([]);
      setSelected(0);
      inputRef.current?.focus();
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const workspacePath = getFeature<WorkspaceApi>(WORKSPACE_KEY).getSnapshot().currentPath;
    if (!workspacePath || query.trim().length < 2) {
      setHits([]);
      return;
    }
    if (debounceRef.current) window.clearTimeout(debounceRef.current);
    debounceRef.current = window.setTimeout(async () => {
      setLoading(true);
      try {
        const result = await getFeature<WorkspaceSearchApi>(WORKSPACE_SEARCH_KEY).search(workspacePath, query);
        setHits(result);
        setSelected(0);
      } finally {
        setLoading(false);
      }
    }, 180);
  }, [query, open]);

  if (!open) return null;

  const pick = (hit: SearchHit) => {
    void getFeature<EditorApi>(EDITOR_KEY).openFile(hit.path);
    onClose();
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelected((s) => Math.min(s + 1, hits.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelected((s) => Math.max(s - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const h = hits[selected];
      if (h) pick(h);
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
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder="Find in path — search text across workspace files"
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
          {loading ? (
            <div
              className="px-3 py-2 text-xs"
              style={{ color: "var(--fg-2)" }}
            >
              Searching…
            </div>
          ) : hits.length === 0 ? (
            <div
              className="px-3 py-2 text-xs"
              style={{ color: "var(--fg-2)" }}
            >
              {query.trim().length < 2
                ? "Type at least two characters to search."
                : "No matches."}
            </div>
          ) : (
            hits.map((hit, i) => (
              <button
                key={`${hit.path}:${hit.line}:${i}`}
                type="button"
                onClick={() => pick(hit)}
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
                    {hit.path.split("/").pop()}
                  </span>
                  <span
                    className="text-xs"
                    style={{ color: "var(--fg-2)" }}
                  >
                    :{hit.line}
                  </span>
                </div>
                <div
                  className="text-xs font-mono truncate"
                  style={{ color: "var(--fg-2)" }}
                >
                  {hit.snippet}
                </div>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
