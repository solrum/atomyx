import { useMemo } from "react";
import { useEditor } from "../../../state/features/editor/index.js";

/**
 * Lightweight YAML outline. Does NOT parse — it scans the raw
 * text for top-level keys and step entries, which is enough to
 * navigate ~95% of Atomyx scripts without pulling in a YAML
 * parser for rendering concerns. Click a row to jump the editor
 * to that line.
 */
export function StructureView({
  onJumpToLine,
}: {
  readonly onJumpToLine: (line: number) => void;
}) {
  const { tabs, activePath } = useEditor();
  const active = tabs.find((t) => t.path === activePath);

  const nodes = useMemo(
    () => (active ? buildOutline(active.content) : []),
    [active?.content],
  );

  if (!active) {
    return (
      <div className="p-3 text-xs" style={{ color: "var(--fg-2)" }}>
        Open a file to see its structure.
      </div>
    );
  }

  return (
    <div className="p-2 space-y-0.5 text-[13px] overflow-y-auto h-full">
      {nodes.length === 0 ? (
        <div className="p-1 text-xs" style={{ color: "var(--fg-2)" }}>
          No structure detected.
        </div>
      ) : (
        nodes.map((n, i) => (
          <button
            key={i}
            type="button"
            onClick={() => onJumpToLine(n.line)}
            className="w-full text-left px-2 py-1 rounded hover:bg-[var(--bg-hover)]"
            style={{ paddingLeft: 8 + n.depth * 12 }}
            title={`Line ${n.line}`}
          >
            <span
              className="mr-1 text-[11px] inline-block w-8 text-right"
              style={{ color: "var(--fg-2)" }}
            >
              {n.line}
            </span>
            <span>{n.label}</span>
          </button>
        ))
      )}
    </div>
  );
}

interface OutlineNode {
  readonly depth: number;
  readonly label: string;
  readonly line: number;
}

function buildOutline(source: string): readonly OutlineNode[] {
  const out: OutlineNode[] = [];
  const lines = source.split("\n");
  let inStepsBlock = false;
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i]!;
    const trimmed = raw.trimStart();
    if (trimmed.length === 0 || trimmed.startsWith("#")) continue;

    // Top-level keys: appId, name, env, steps, tags, requires, etc.
    const topLevel = /^(appId|name|description|precondition|tags|env|proxy|requires|stepDelay|format|steps):/.exec(
      raw,
    );
    if (topLevel && raw.startsWith(topLevel[1]!)) {
      out.push({ depth: 0, label: topLevel[1]!, line: i + 1 });
      inStepsBlock = topLevel[1] === "steps";
      continue;
    }

    if (raw === "---") {
      out.push({ depth: 0, label: "— steps —", line: i + 1 });
      inStepsBlock = true;
      continue;
    }

    if (inStepsBlock) {
      const stepMatch = /^(\s*)-\s*(.+?)\s*$/.exec(raw);
      if (stepMatch) {
        const indent = stepMatch[1]!.length;
        if (indent <= 2) {
          const body = stepMatch[2]!;
          const command = body.split(/[:\s]/)[0] ?? body;
          out.push({ depth: 1, label: `${command}`, line: i + 1 });
        }
      }
    }
  }
  return out;
}
