import {
  CheckCircle2,
  XCircle,
  Circle,
  AlertTriangle,
  Loader2,
  CircleDashed,
} from "lucide-react";
import { useRuns } from "../../../state/features/runs/index.js";
import type {
  ScenarioScriptRow,
  ScenarioScriptStatus,
} from "../../../state/features/runs/runs.contract.js";

/**
 * Bottom-pane body that surfaces the per-script progress of an
 * active scenario run. Empty placeholder otherwise — the pane is
 * useful precisely when a scenario is in flight, but stays
 * accessible from the stripe so the user can re-open it after
 * the run completes to review which script failed.
 */
export function ScenarioProgress() {
  const { live } = useRuns();
  const scenario = live?.scenario;

  if (!scenario) {
    return (
      <div
        style={{
          padding: "var(--gap-4) var(--gap-5)",
          fontSize: "var(--fs-12)",
          color: "var(--fg-3)",
        }}
      >
        No scenario active. Open a `.scenario.yml` file and hit Run to
        see per-script progress here.
      </div>
    );
  }

  const totalDuration = scenario.scripts.reduce(
    (sum, s) => sum + (s.durationMs ?? 0),
    0,
  );
  const passed = scenario.scripts.filter((s) => s.status === "passed").length;
  const failed = scenario.scripts.filter(
    (s) => s.status === "failed" || s.status === "errored",
  ).length;
  const skipped = scenario.scripts.filter((s) => s.status === "skipped").length;

  return (
    <div className="flex flex-col h-full">
      <header
        className="flex items-baseline"
        style={{
          gap: "var(--gap-3)",
          padding: "var(--gap-3) var(--gap-5)",
          borderBottom: "1px solid var(--line)",
          fontSize: "var(--fs-12)",
        }}
      >
        <span style={{ color: "var(--fg-1)", fontWeight: 600 }}>
          {scenario.name}
        </span>
        <span style={{ color: "var(--fg-3)" }}>
          {passed} passed
          {failed > 0 ? ` · ${failed} failed` : ""}
          {skipped > 0 ? ` · ${skipped} skipped` : ""}
          {" · "}
          {scenario.totalScripts} total
        </span>
        <span style={{ marginLeft: "auto", color: "var(--fg-3)" }}>
          {formatTime(totalDuration)}
        </span>
      </header>
      <ul
        className="flex-1 min-h-0 overflow-auto"
        style={{ padding: "var(--gap-2) 0" }}
      >
        {scenario.scripts.map((row) => (
          <ScenarioRow key={row.index} row={row} />
        ))}
      </ul>
    </div>
  );
}

function ScenarioRow({ row }: { readonly row: ScenarioScriptRow }) {
  return (
    <li
      className="flex items-center"
      style={{
        gap: "var(--gap-3)",
        padding: "var(--gap-2) var(--gap-5)",
        height: "var(--row-h, 22px)",
        fontSize: "var(--fs-12)",
        color: "var(--fg-1)",
      }}
    >
      <StatusIcon status={row.status} />
      <span
        className="flex-none"
        style={{
          fontFamily: "var(--font-mono)",
          color: "var(--fg-3)",
          minWidth: "1.5rem",
          textAlign: "right",
        }}
      >
        {row.index + 1}.
      </span>
      <span className="flex-1 min-w-0 truncate">
        {row.path || `(script ${row.index + 1})`}
      </span>
      {row.failedAtStep !== undefined ? (
        <span
          className="flex-none"
          style={{
            fontSize: "var(--fs-11)",
            fontFamily: "var(--font-mono)",
            color: "var(--err)",
          }}
        >
          step {row.failedAtStep + 1} failed
        </span>
      ) : null}
      {row.durationMs !== undefined && row.durationMs > 0 ? (
        <span
          className="flex-none"
          style={{
            fontSize: "var(--fs-11)",
            fontFamily: "var(--font-mono)",
            color: "var(--fg-3)",
          }}
        >
          {formatTime(row.durationMs)}
        </span>
      ) : null}
    </li>
  );
}

function StatusIcon({ status }: { readonly status: ScenarioScriptStatus }) {
  const cls = "h-3.5 w-3.5 flex-none";
  switch (status) {
    case "passed":
      return <CheckCircle2 className={cls} style={{ color: "var(--ok)" }} />;
    case "failed":
      return <XCircle className={cls} style={{ color: "var(--err)" }} />;
    case "errored":
      return (
        <AlertTriangle className={cls} style={{ color: "var(--warn)" }} />
      );
    case "running":
      return (
        <Loader2
          className={`${cls} animate-spin`}
          style={{ color: "var(--accent)" }}
        />
      );
    case "skipped":
      return (
        <CircleDashed className={cls} style={{ color: "var(--fg-3)" }} />
      );
    default:
      return <Circle className={cls} style={{ color: "var(--fg-3)" }} />;
  }
}

function formatTime(ms: number): string {
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}
