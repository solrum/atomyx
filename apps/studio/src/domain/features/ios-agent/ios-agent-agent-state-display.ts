import type { IosAgentState, IosAgentStatus } from "./ios-agent.port.js";

/**
 * Display strings + theme-token colours for the iOS agent status
 * chip. Centralised here so future surfaces (welcome banner,
 * device picker, run guard) render the same labels and colours
 * without each maintaining its own switch.
 *
 * Colour values are CSS custom-property expressions with a fall-
 * back hex literal so a theme that does not define the token
 * still renders a legible chip.
 */
export interface AgentStateDisplay {
  readonly label: string;
  readonly color: string;
}

const DEFAULT_PORT = 22087;

export function agentStateLabel(state: IosAgentState): string {
  switch (state) {
    case "ready":
      return "Agent ready";
    case "building":
      return "Agent starting…";
    case "failed":
      return "Agent failed";
    case "idle":
      return "Agent idle";
  }
}

export function agentStateColor(state: IosAgentState): string {
  switch (state) {
    case "ready":
      return "var(--ui-success, #6a8759)";
    case "building":
      return "#eab308";
    case "failed":
      return "var(--ui-danger, #f87171)";
    case "idle":
      return "var(--ui-text-muted)";
  }
}

export function describeAgentState(state: IosAgentState): AgentStateDisplay {
  return { label: agentStateLabel(state), color: agentStateColor(state) };
}

/**
 * Build the chip's tooltip. When the runner has surfaced a
 * `message`, prefer it; otherwise fall back to the label plus
 * the port the bridge listens on.
 */
export function agentStateTooltip(status: IosAgentStatus | null): string {
  const state = status?.state ?? "idle";
  const label = agentStateLabel(state);
  if (status?.message) return `${label} — ${status.message}`;
  return `${label} (port ${status?.port ?? DEFAULT_PORT})`;
}
