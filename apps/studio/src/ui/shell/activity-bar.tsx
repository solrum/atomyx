import type { ReactNode } from "react";
import {
  Files,
  PlaySquare,
  Settings as SettingsIcon,
} from "lucide-react";
import { useLayout } from "../../state/features/layout/index.js";
import {
  problemCounts,
  useProblems,
} from "../../state/features/problems/index.js";

interface Activity {
  readonly id: string;
  readonly icon: ReactNode;
  readonly label: string;
  readonly shortcut?: string;
  readonly active: boolean;
  readonly badge?: number;
  readonly badgeTone?: "danger" | "warn" | "info";
  readonly onClick: () => void;
}

/**
 * Left-side 44px activity rail. Files and Runs at the top,
 * Settings pinned at the bottom.
 */
export function ActivityBar() {
  const layout = useLayout();
  const { items: problems } = useProblems();
  const counts = problemCounts(problems);
  const errCount = counts.errors;
  const warnCount = counts.warnings;
  const top: readonly Activity[] = [
    {
      id: "files",
      icon: <Files style={iconStyle} />,
      label: "Files",
      shortcut: "⌘1",
      active: layout.fileTreeVisible,
      onClick: layout.toggleFileTree,
    },
    {
      id: "runs",
      icon: <PlaySquare style={iconStyle} />,
      label: "Runs",
      shortcut: "⌘3",
      active: layout.runDrawerVisible,
      badge: errCount + warnCount > 0 ? errCount + warnCount : undefined,
      badgeTone: errCount > 0 ? "danger" : warnCount > 0 ? "warn" : undefined,
      onClick: layout.toggleRunDrawer,
    },
  ];
  const bottom: readonly Activity[] = [
    {
      id: "settings",
      icon: <SettingsIcon style={iconStyle} />,
      label: "Settings",
      shortcut: "⌘,",
      active: layout.settingsViewVisible,
      onClick: layout.toggleSettingsView,
    },
  ];

  return (
    <nav aria-label="Activity bar" className="activity-bar">
      {top.map((a) => (
        <ActivityButton key={a.id} activity={a} />
      ))}
      <div className="activity-spacer" />
      {bottom.map((a) => (
        <ActivityButton key={a.id} activity={a} />
      ))}
    </nav>
  );
}

const iconStyle = { width: "18px", height: "18px" } as const;

function ActivityButton({ activity }: { readonly activity: Activity }) {
  const title = activity.shortcut
    ? `${activity.label} — ${activity.shortcut}`
    : activity.label;
  const toneColor =
    activity.badgeTone === "danger"
      ? "var(--err)"
      : activity.badgeTone === "warn"
        ? "var(--warn)"
        : "var(--accent)";
  return (
    <button
      type="button"
      onClick={activity.onClick}
      title={title}
      aria-label={activity.label}
      aria-pressed={activity.active}
      className={activity.active ? "activity-btn active" : "activity-btn"}
    >
      {activity.icon}
      {activity.badge != null ? (
        <span
          aria-hidden
          style={{
            position: "absolute",
            top: "2px",
            right: "2px",
            minWidth: "12px",
            height: "12px",
            padding: "0 3px",
            borderRadius: "var(--r-pill)",
            background: toneColor,
            color: "#0e1e33",
            fontSize: "9px",
            fontWeight: 700,
            display: "grid",
            placeItems: "center",
            lineHeight: 1,
          }}
        >
          {activity.badge > 99 ? "99+" : activity.badge}
        </span>
      ) : null}
    </button>
  );
}
