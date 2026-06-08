import { useEffect, useRef } from "react";
import { Trash2 } from "lucide-react";
import {
  useLogs,
  type LogEntry,
  type LogLevel,
  type LogSource,
} from "../../../state/features/logs/index.js";

const LEVEL_OPTIONS: readonly LogLevel[] = ["debug", "info", "warn", "error"];

export function LogsList() {
  const snap = useLogs();
  const filtered = snap.filteredEntries;

  const scrollerRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!snap.autoScroll) return;
    const el = scrollerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [filtered, snap.autoScroll]);

  return (
    <div className="flex h-full flex-col">
      <Toolbar
        knownSources={snap.knownSources}
        source={snap.filter.source}
        minLevel={snap.filter.minLevel}
        search={snap.filter.search}
        autoScroll={snap.autoScroll}
        onSource={(v) => snap.setFilter({ source: v })}
        onLevel={(v) => snap.setFilter({ minLevel: v })}
        onSearch={(v) => snap.setFilter({ search: v })}
        onAutoScroll={(v) => snap.setAutoScroll(v)}
        onClear={() => snap.clear()}
        total={snap.items.length}
        shown={filtered.length}
      />
      <div
        ref={scrollerRef}
        className="flex-1 overflow-y-auto font-mono text-[11px] leading-tight"
      >
        {filtered.length === 0 ? (
          <div
            className="px-3 py-2 text-xs"
            style={{ color: "var(--fg-2)" }}
          >
            No log entries match the current filter.
          </div>
        ) : (
          filtered.map((e) => <LogRow key={e.id} entry={e} />)
        )}
      </div>
    </div>
  );
}

interface ToolbarProps {
  readonly knownSources: readonly LogSource[];
  readonly source: LogSource | "all";
  readonly minLevel: LogLevel;
  readonly search: string;
  readonly autoScroll: boolean;
  readonly total: number;
  readonly shown: number;
  readonly onSource: (s: LogSource | "all") => void;
  readonly onLevel: (l: LogLevel) => void;
  readonly onSearch: (q: string) => void;
  readonly onAutoScroll: (on: boolean) => void;
  readonly onClear: () => void;
}

function Toolbar(p: ToolbarProps) {
  return (
    <div
      className="flex flex-none items-center gap-2 border-b px-2 py-1 text-[11px]"
      style={{ borderColor: "var(--line)" }}
    >
      <select
        value={p.source}
        onChange={(e) => p.onSource(e.target.value as LogSource | "all")}
        className="bg-transparent px-1 py-0.5 text-[11px]"
        style={{
          color: "var(--fg-0)",
          border: "1px solid var(--line)",
        }}
      >
        <option value="all">all sources</option>
        {p.knownSources.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </select>
      <select
        value={p.minLevel}
        onChange={(e) => p.onLevel(e.target.value as LogLevel)}
        className="bg-transparent px-1 py-0.5 text-[11px]"
        style={{
          color: "var(--fg-0)",
          border: "1px solid var(--line)",
        }}
      >
        {LEVEL_OPTIONS.map((l) => (
          <option key={l} value={l}>
            {l}+
          </option>
        ))}
      </select>
      <input
        type="text"
        placeholder="search messages"
        value={p.search}
        onChange={(e) => p.onSearch(e.target.value)}
        className="flex-1 bg-transparent px-2 py-0.5 text-[11px]"
        style={{
          color: "var(--fg-0)",
          border: "1px solid var(--line)",
        }}
      />
      <label
        className="flex items-center gap-1 text-[11px]"
        style={{ color: "var(--fg-2)" }}
      >
        <input
          type="checkbox"
          checked={p.autoScroll}
          onChange={(e) => p.onAutoScroll(e.target.checked)}
        />
        auto-scroll
      </label>
      <span className="text-[10px]" style={{ color: "var(--fg-2)" }}>
        {p.shown}/{p.total}
      </span>
      <button
        type="button"
        onClick={p.onClear}
        title="Clear logs"
        className="p-1 hover:bg-[var(--bg-hover)]"
        style={{ color: "var(--fg-2)" }}
      >
        <Trash2 className="h-3 w-3" />
      </button>
    </div>
  );
}

function LogRow({ entry }: { readonly entry: LogEntry }) {
  const color = colorFor(entry.level);
  const time = new Date(entry.ts).toISOString().slice(11, 23);
  return (
    <div
      className="flex gap-2 whitespace-pre-wrap break-all px-2 py-[1px] hover:bg-[var(--bg-hover)]"
      style={{ color: "var(--fg-0)" }}
    >
      <span style={{ color: "var(--fg-2)" }}>{time}</span>
      <span style={{ color }} className="w-10 flex-none uppercase">
        {entry.level}
      </span>
      <span style={{ color: "var(--fg-2)" }} className="flex-none">
        [{entry.source}]
      </span>
      <span className="flex-1">{entry.message}</span>
    </div>
  );
}

function colorFor(level: LogLevel): string {
  switch (level) {
    case "error":
      return "var(--err)";
    case "warn":
      return "var(--warn)";
    case "debug":
      return "var(--fg-2)";
    default:
      return "var(--fg-0)";
  }
}
