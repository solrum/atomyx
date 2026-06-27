import type { ReactNode } from "react";
import {
  AlertCircle,
  AlertTriangle,
  GitBranch,
  Check,
  Sparkles,
} from "lucide-react";
import {
  useProblems,
  problemCounts,
} from "../../state/features/problems/index.js";
import { useEditor } from "../../state/features/editor/index.js";
import { useWorkspace } from "../../state/features/workspace/index.js";
import { useRuntimeStatus } from "../../state/features/runtime-status/index.js";
import { useLayout } from "../../state/features/layout/index.js";

export function StatusBar() {
  const { currentPath } = useWorkspace();
  const { activePath } = useEditor();
  const { items } = useProblems();
  const runtime = useRuntimeStatus();
  const { viewMode } = useLayout();
  const counts = problemCounts(items);

  const mcpLabel =
    runtime.status === "connected"
      ? "atomyx-mcp connected"
      : runtime.status === "connecting"
        ? "atomyx-mcp connecting…"
        : "atomyx-mcp offline";
  const mcpColor =
    runtime.status === "connected"
      ? "var(--ok)"
      : runtime.status === "connecting"
        ? "var(--warn)"
        : "var(--err)";

  const yamlValid = activePath !== null && counts.errors === 0;

  return (
    <div className="statusbar select-none" style={{ height: "22px" }}>
      <StatusItem
        title={currentPath ?? "No folder opened"}
        color="var(--fg-2)"
      >
        <GitBranch style={iconXs} />
        <span>main</span>
      </StatusItem>
      {activePath ? (
        <StatusItem
          title={yamlValid ? "Schema valid" : `${counts.errors} schema errors`}
          color={yamlValid ? "var(--ok)" : "var(--err)"}
        >
          {yamlValid ? (
            <Check style={iconXs} />
          ) : (
            <AlertCircle style={iconXs} />
          )}
          <span>YAML {yamlValid ? "schema valid" : "errors"}</span>
        </StatusItem>
      ) : null}
      <StatusItem
        color={counts.errors > 0 ? "var(--err)" : "var(--fg-3)"}
        title={`${counts.errors} error${counts.errors === 1 ? "" : "s"}`}
      >
        <AlertCircle style={iconXs} />
        <span>{counts.errors}</span>
      </StatusItem>
      <StatusItem
        color={counts.warnings > 0 ? "var(--warn)" : "var(--fg-3)"}
        title={`${counts.warnings} warning${counts.warnings === 1 ? "" : "s"}`}
      >
        <AlertTriangle style={iconXs} />
        <span>{counts.warnings}</span>
      </StatusItem>
      <div className="spacer" />
      <StatusItem color={mcpColor} title={runtime.lastError ?? mcpLabel}>
        <span
          aria-hidden
          style={{
            width: "6px",
            height: "6px",
            borderRadius: "50%",
            background: mcpColor,
          }}
        />
        <span>{mcpLabel}</span>
      </StatusItem>
      <StatusItem color="var(--fg-3)">
        <span>UTF-8</span>
      </StatusItem>
      <StatusItem color="var(--fg-3)">
        <span>LF</span>
      </StatusItem>
      {activePath ? (
        <StatusItem color="var(--fg-3)">
          <span>YAML</span>
        </StatusItem>
      ) : null}
      <button
        type="button"
        className="hover"
        title="Tweaks"
        aria-label="Tweaks"
        onClick={() =>
          window.dispatchEvent(new CustomEvent("atomyx:tweaks:toggle"))
        }
      >
        <Sparkles style={iconXs} />
      </button>
      <span className={`sb-mode ${viewMode}`} title={`${cap(viewMode)} mode`}>
        <span className="sb-mode-dot" aria-hidden />
        {cap(viewMode)}
      </span>
    </div>
  );
}

function cap(s: string): string {
  return s.length === 0 ? s : s[0]!.toUpperCase() + s.slice(1);
}

const iconXs = { width: "11px", height: "11px" } as const;

function StatusItem({
  color,
  title,
  children,
}: {
  readonly color: string;
  readonly title?: string;
  readonly children: ReactNode;
}) {
  return (
    <div className="item" title={title} style={{ color }}>
      {children}
    </div>
  );
}
