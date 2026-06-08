import { Play, Square } from "lucide-react";
import { getFeature } from "../../../state/core/registry.js";
import { Button } from "../../primitives/button.js";
import { useRuns } from "../../../state/features/runs/index.js";
import type { ActionsApi } from "../../../state/features/actions/index.js";
import { ACTIONS_KEY } from "../../../state/features/actions/index.js";

export function RunControls() {
  const { live } = useRuns();
  const running = live !== null && live.result === "running";

  if (running) {
    return (
      <Button
        variant="ghost"
        size="sm"
        onClick={() => void getFeature<ActionsApi>(ACTIONS_KEY).execute("run.stop")}
        title="Stop Running Script"
      >
        <Square className="h-3 w-3 mr-1" style={{ color: "var(--err)" }} />{" "}
        Stop
      </Button>
    );
  }
  return (
    <Button
      variant="secondary"
      size="sm"
      onClick={() => void getFeature<ActionsApi>(ACTIONS_KEY).execute("run.start")}
      title="Run Active Script — ⌘⇧P"
    >
      <Play className="h-3 w-3 mr-1" /> Run
    </Button>
  );
}
