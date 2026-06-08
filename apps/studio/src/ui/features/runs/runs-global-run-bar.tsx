import { useMemo } from "react";
import { Play, CircleStop, RefreshCw, Clock } from "lucide-react";
import { cn } from "../../primitives/index.js";
import {
  useRuns,
  type LiveRun,
} from "../../../state/features/runs/index.js";
import { useEditor } from "../../../state/features/editor/index.js";
import { useDevices } from "../../../state/features/devices/index.js";
import { jumpActiveEditorTo } from "../editor/index.js";

type RunState = "idle" | "running" | "pass" | "fail";

interface TimelineEntry {
  readonly index: number;
  readonly command: string;
  readonly line: number | null;
  readonly state: "pending" | "run" | "pass" | "fail";
}

/**
 * Run bar pinned above the editor. Mirrors the v5 design's
 * `gr-bar`: primary play/pause/stop control, status pill, mini
 * timeline track that scrubs the editor caret to the step's line,
 * step detail, and elapsed time. Drives the same `useRuns` store
 * the right-pane RunPanel consumes — the two surfaces stay in
 * lock-step without wiring duplicate state.
 */
export function GlobalRunBar() {
  const { live, startRun, stopRun } = useRuns();
  const { tabs, activePath } = useEditor();
  const { devices } = useDevices();
  const active = tabs.find((t) => t.path === activePath);
  const state = deriveState(live);
  const timeline = useTimeline(live);
  const elapsed = elapsedMs(live);
  const cur =
    state === "idle"
      ? null
      : timeline[Math.min(currentIdx(timeline, state), timeline.length - 1)] ??
        null;

  const canStop = state === "running";
  const canRun =
    !!active && devices.length > 0 && state !== "running";

  function onPlay() {
    if (!active || devices.length === 0) return;
    void startRun(active.path, active.content, {
      deviceId: devices[0]!.id,
    });
  }

  return (
    <div className="gr-bar" data-state={state}>
      <button
        type="button"
        className="gr-btn primary"
        onClick={onPlay}
        disabled={!canRun}
        title={canRun ? "Run (F5)" : "Open a script and connect a device"}
      >
        <Play /> Run
      </button>

      <button
        type="button"
        className={cn("gr-btn icon", canStop && "danger")}
        disabled={!canStop}
        onClick={stopRun}
        title="Stop"
      >
        <CircleStop />
      </button>

      <button
        type="button"
        className="gr-btn icon"
        disabled={state === "idle" || !active || devices.length === 0}
        onClick={onPlay}
        title="Replay"
      >
        <RefreshCw />
      </button>

      <span className="gr-sep" />

      <span className={`gr-status-pill ${state}`}>
        <span className="gr-pulse" />
        {labelFor(state)}
      </span>

      {timeline.length > 0 ? (
        <div className="gr-mini-track" title="Timeline (click to scrub)">
          {timeline.map((s) => (
            <div
              key={s.index}
              className={cn(
                "gr-mini-tick",
                s.state === "pass" && "passed",
                s.state === "fail" && "failed",
                s.state === "run" && "current",
              )}
              title={
                s.line !== null
                  ? `step ${s.index + 1}: ${s.command} (line ${s.line})`
                  : `step ${s.index + 1}: ${s.command}`
              }
              onClick={() => s.line !== null && jumpActiveEditorTo(s.line)}
            />
          ))}
        </div>
      ) : null}

      {cur ? (
        <span className="gr-step-info">
          <span>
            step{" "}
            <b>
              {cur.index + 1}/{timeline.length}
            </b>
          </span>
          <span className="gr-cmd">{cur.command}</span>
          {cur.line !== null ? (
            <span style={{ color: "var(--fg-3)" }}>· line {cur.line}</span>
          ) : null}
        </span>
      ) : null}

      <span className="spacer" />

      {state !== "idle" && elapsed != null ? (
        <span className="gr-elapsed" title="Elapsed time">
          <Clock size={11} />
          <span>{formatTime(elapsed)}</span>
        </span>
      ) : null}
    </div>
  );
}

function deriveState(live: LiveRun | null): RunState {
  if (!live) return "idle";
  if (live.result === "running") return "running";
  if (live.result === "passed") return "pass";
  if (live.result === "failed") return "fail";
  return "idle";
}

function labelFor(state: RunState): string {
  switch (state) {
    case "running":
      return "Running";
    case "pass":
      return "Passed";
    case "fail":
      return "Failed";
    default:
      return "Idle";
  }
}

function elapsedMs(live: LiveRun | null): number | null {
  if (!live) return null;
  for (const e of live.events) {
    if (e.type === "runCompleted") return e.completedAt - live.startedAt;
  }
  return Date.now() - live.startedAt;
}

function formatTime(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function currentIdx(timeline: readonly TimelineEntry[], state: RunState): number {
  if (state === "idle") return 0;
  if (state === "pass") return timeline.length - 1;
  if (state === "fail") {
    const failed = timeline.findIndex((s) => s.state === "fail");
    return failed >= 0 ? failed : timeline.length - 1;
  }
  const running = timeline.findIndex((s) => s.state === "run");
  return running >= 0 ? running : 0;
}

function useTimeline(live: LiveRun | null): readonly TimelineEntry[] {
  return useMemo<readonly TimelineEntry[]>(() => {
    if (!live) return [];
    const started: { index: number; command: string }[] = [];
    const completed = new Map<
      number,
      { ok: boolean }
    >();
    for (const e of live.events) {
      if (e.type === "stepStarted") {
        started.push({
          index: e.stepIndex,
          command: e.command,
        });
      } else if (e.type === "stepCompleted") {
        completed.set(e.stepIndex, { ok: e.ok });
      }
    }
    const runIdx = started.findIndex((s) => !completed.has(s.index));
    return started.map<TimelineEntry>((s, i) => {
      const done = completed.get(s.index);
      const stateValue: TimelineEntry["state"] = done
        ? done.ok
          ? "pass"
          : "fail"
        : i === runIdx
          ? "run"
          : "pending";
      return {
        index: s.index,
        command: s.command,
        line: null,
        state: stateValue,
      };
    });
  }, [live]);
}
