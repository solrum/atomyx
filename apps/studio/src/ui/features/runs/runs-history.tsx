import { useEffect } from "react";
import { CheckCircle2, XCircle, Circle, Trash2 } from "lucide-react";
import { useRuns } from "../../../state/features/runs/index.js";
import type { RunMetadata } from "../../../domain/features/artifacts/index.js";
import { formatRelativeTime } from "../../../domain/features/runs/index.js";

export function RunsHistory() {
  const { history: items, loadHistory } = useRuns();

  useEffect(() => {
    void loadHistory();
  }, [loadHistory]);

  if (items.length === 0) {
    return (
      <div
        style={{
          padding: "var(--gap-4) var(--gap-5)",
          fontSize: "var(--fs-12)",
          color: "var(--fg-3)",
        }}
      >
        No past runs yet. Runs are recorded when you hit the Run button.
      </div>
    );
  }

  return (
    <ul style={{ padding: "var(--gap-2) 0" }}>
      {items.map((r) => (
        <RunRow key={r.runId} run={r} />
      ))}
    </ul>
  );
}

function RunRow({ run }: { readonly run: RunMetadata }) {
  const { deleteRun } = useRuns();
  const icon =
    run.result === "passed" ? (
      <CheckCircle2
        className="h-3 w-3 flex-none"
        style={{ color: "var(--ok)" }}
      />
    ) : run.result === "failed" ? (
      <XCircle className="h-3 w-3 flex-none" style={{ color: "var(--err)" }} />
    ) : (
      <Circle
        className="h-3 w-3 flex-none"
        style={{ color: "var(--fg-3)" }}
      />
    );

  const onDelete = async (e: React.MouseEvent) => {
    e.stopPropagation();
    await deleteRun(run.runId);
  };

  return (
    <li
      className="run-row flex items-center"
      style={{
        gap: "var(--gap-3)",
        padding: "var(--gap-2) var(--gap-5)",
        height: "var(--row-h, 22px)",
        fontSize: "var(--fs-12)",
        color: "var(--fg-1)",
      }}
    >
      {icon}
      <span className="flex-1 min-w-0 truncate">{run.scriptName}</span>
      <span
        className="flex-none"
        style={{
          fontSize: "var(--fs-11)",
          fontFamily: "var(--font-mono)",
          color: "var(--fg-3)",
        }}
        title={new Date(run.startedAt).toLocaleString()}
      >
        {formatRelativeTime(run.startedAt)}
      </span>
      {run.totalSteps !== undefined ? (
        <span
          className="flex-none"
          style={{
            fontSize: "var(--fs-11)",
            fontFamily: "var(--font-mono)",
            color: "var(--fg-3)",
          }}
        >
          {run.failedAtStep !== undefined
            ? `${run.failedAtStep + 1}/${run.totalSteps}`
            : `${run.totalSteps} steps`}
        </span>
      ) : null}
      <button
        type="button"
        aria-label="Delete run"
        onClick={(e) => void onDelete(e)}
        className="opacity-40 hover:opacity-100 flex-none"
        style={{ color: "var(--fg-2)" }}
      >
        <Trash2 className="h-3 w-3" />
      </button>
    </li>
  );
}

