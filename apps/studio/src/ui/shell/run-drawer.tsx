import { useState } from "react";
import { ChevronDown, Files as FilesIcon, ListChecks, Terminal } from "lucide-react";
import { useRuns } from "../../state/features/runs/index.js";
import { getFeature } from "../../state/core/registry.js";
import type { LayoutApi } from "../../state/features/layout/index.js";
import { LAYOUT_KEY, useLayout } from "../../state/features/layout/index.js";
import {
  RunArtifacts,
  RunConsole,
  RunTimeline,
  artifactCount,
  consoleLineCount,
  deriveRunState,
  stepCount,
  type RunState,
} from "../features/run-views/index.js";
import { cn } from "../primitives/index.js";

type DrawerTab = "timeline" | "console" | "artifacts";

/**
 * Run drawer docked at the bottom of the editor area. The header is a
 * `.slot-header` with the RUN label + a status badge mirroring the
 * GlobalRunBar; clicking the header collapses the drawer to its 30px
 * chrome. The body switches between Timeline / Console / Artifacts tab
 * panels — those views are pure consumers of the live-run feed and
 * carry no run-controls of their own (GlobalRunBar owns play/stop).
 */
export function RunDrawer() {
  const { live } = useRuns();
  const { runDrawerCollapsed } = useLayout();
  const [tab, setTab] = useState<DrawerTab>("timeline");
  const state = deriveRunState(live);

  return (
    <div className={cn("slot run-drawer", runDrawerCollapsed && "collapsed")}>
      <div
        className="slot-header"
        role="button"
        tabIndex={0}
        onClick={(e) => {
          if ((e.target as HTMLElement).closest(".rt-tab")) return;
          if ((e.target as HTMLElement).closest(".icon-btn")) return;
          getFeature<LayoutApi>(LAYOUT_KEY).toggleRunDrawerCollapsed();
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            getFeature<LayoutApi>(LAYOUT_KEY).toggleRunDrawerCollapsed();
          }
        }}
      >
        <span className="caret" aria-hidden>
          <ChevronDown style={{ width: 10, height: 10 }} />
        </span>
        <span>Run</span>
        <RunBadge state={state} />
        <span className="rd-tabs">
          <DrawerTabBtn
            active={tab === "timeline"}
            onClick={() => setTab("timeline")}
            icon={<ListChecks style={iconXs} />}
            label="Timeline"
            count={stepCount(live)}
          />
          <DrawerTabBtn
            active={tab === "console"}
            onClick={() => setTab("console")}
            icon={<Terminal style={iconXs} />}
            label="Console"
            count={consoleLineCount(live)}
          />
          <DrawerTabBtn
            active={tab === "artifacts"}
            onClick={() => setTab("artifacts")}
            icon={<FilesIcon style={iconXs} />}
            label="Artifacts"
            count={artifactCount(live)}
          />
        </span>
        <span className="spacer" />
      </div>
      <div className="slot-body run-body">
        {tab === "timeline" ? (
          <RunTimeline live={live} state={state} />
        ) : tab === "console" ? (
          <RunConsole live={live} />
        ) : (
          <RunArtifacts live={live} />
        )}
      </div>
    </div>
  );
}

const iconXs = { width: 10, height: 10 } as const;

interface DrawerTabBtnProps {
  readonly active: boolean;
  readonly onClick: () => void;
  readonly icon: React.ReactNode;
  readonly label: string;
  readonly count: number;
}

function DrawerTabBtn({ active, onClick, icon, label, count }: DrawerTabBtnProps) {
  return (
    <button
      type="button"
      className={cn("rt-tab", active && "active")}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
    >
      {icon}
      {label}
      {count > 0 ? <span className="rt-count">{count}</span> : null}
    </button>
  );
}

interface RunBadgeProps {
  readonly state: RunState;
}

function RunBadge({ state }: RunBadgeProps) {
  const { background, color, label } = badgeStyle(state);
  return (
    <span
      className="badge-mode"
      style={{ background, color, borderColor: "transparent" }}
    >
      {label}
    </span>
  );
}

function badgeStyle(state: RunState): {
  readonly background: string;
  readonly color: string;
  readonly label: string;
} {
  switch (state) {
    case "running":
      return {
        background: "var(--accent-bg)",
        color: "var(--accent)",
        label: "Running",
      };
    case "pass":
      return {
        background: "color-mix(in oklab, var(--ok) 18%, transparent)",
        color: "var(--ok)",
        label: "Pass",
      };
    case "fail":
      return {
        background: "var(--err-bg)",
        color: "var(--err)",
        label: "Fail",
      };
    default:
      return { background: "var(--bg-3)", color: "var(--fg-3)", label: "Idle" };
  }
}
