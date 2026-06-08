import { Terminal } from "lucide-react";
import type { LiveRun } from "../../../state/features/runs/index.js";
import { formatHHMMSS } from "./run-views.js";

interface ConsoleViewProps {
  readonly live: LiveRun | null;
}

export function RunConsole({ live }: ConsoleViewProps) {
  if (!live) {
    return <Empty>Run the script to see console output.</Empty>;
  }
  const logs = live.events.filter((e) => e.type === "consoleLog");
  if (logs.length === 0) {
    return <Empty>No console output yet.</Empty>;
  }
  return (
    <div className="console">
      <div className="console-header">
        <Terminal style={{ width: "11px", height: "11px" }} />
        CONSOLE
        <span style={{ flex: 1 }} />
        <span className="count">{logs.length} lines</span>
      </div>
      <div className="console-body">
        {logs.map((e, i) =>
          e.type === "consoleLog" ? (
            <div
              key={i}
              className={
                e.level === "error"
                  ? "l-err"
                  : e.level === "warn"
                    ? "l-warn"
                    : "l-info"
              }
            >
              <span className="t">{formatHHMMSS(e.at)}</span>
              <span className="tag">{e.level}</span>
              <span>{e.line}</span>
            </div>
          ) : null,
        )}
      </div>
    </div>
  );
}

function Empty({ children }: { readonly children: React.ReactNode }) {
  return (
    <div
      style={{
        padding: "var(--gap-5)",
        fontSize: "var(--fs-12)",
        color: "var(--fg-3)",
      }}
    >
      {children}
    </div>
  );
}
