import { useRuntimeStatus } from "../../../state/features/runtime-status/index.js";

export function RuntimeStatusIndicator() {
  const s = useRuntimeStatus();

  const color =
    s.status === "connected"
      ? "var(--ok)"
      : s.status === "connecting"
        ? "#fbbf24"
        : "var(--err)";
  const title =
    s.status === "connected"
      ? "Runtime connected"
      : s.status === "connecting"
        ? "Connecting to runtime…"
        : `Runtime disconnected${s.lastError ? `: ${s.lastError}` : ""}`;

  return (
    <div
      className="h-2 w-2 rounded-full"
      title={title}
      style={{
        background: color,
        boxShadow: s.status === "connected" ? `0 0 6px ${color}` : undefined,
      }}
    />
  );
}
