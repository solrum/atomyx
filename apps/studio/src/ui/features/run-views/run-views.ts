import type { LiveRun } from "../../../state/features/runs/index.js";

export type RunState = "idle" | "running" | "pass" | "fail";

export function deriveRunState(live: LiveRun | null): RunState {
  if (!live) return "idle";
  if (live.result === "running") return "running";
  if (live.result === "passed") return "pass";
  if (live.result === "failed") return "fail";
  return "idle";
}

export function elapsedMs(live: LiveRun | null): number | null {
  if (!live) return null;
  for (const e of live.events) {
    if (e.type === "runCompleted") return e.completedAt - live.startedAt;
  }
  return Date.now() - live.startedAt;
}

export function artifactCount(live: LiveRun | null): number {
  if (!live) return 0;
  return live.events.filter((e) => e.type === "screenshot").length;
}

export function consoleLineCount(live: LiveRun | null): number {
  if (!live) return 0;
  return live.events.filter((e) => e.type === "consoleLog").length;
}

export function stepCount(live: LiveRun | null): number {
  if (!live) return 0;
  return live.events.filter((e) => e.type === "stepStarted").length;
}

export function basename(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

export function formatHHMMSS(ts: number): string {
  const d = new Date(ts);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
