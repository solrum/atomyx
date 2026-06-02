import { createElement } from "react";
import { ListChecks } from "lucide-react";
import { getFeature } from "../../../state/core/registry.js";
import { toolWindowRegistry } from "../../shell/tool-window-registry.js";
import type { LayoutApi } from "../../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../../state/features/layout/index.js";
import type { RunsApi } from "../../../state/features/runs/index.js";
import { RUNS_KEY } from "../../../state/features/runs/index.js";
import { ScenarioProgress } from "./scenario-progress.js";

toolWindowRegistry.register({
  id: "scenario",
  side: "bottom",
  icon: createElement(ListChecks, { className: "h-3.5 w-3.5" }),
  label: "Scenario",
  isVisible: () => {
    const s = getFeature<LayoutApi>(LAYOUT_KEY).getSnapshot();
    return s.problemsVisible && s.bottomPane === "scenario";
  },
  toggle: () => {
    getFeature<LayoutApi>(LAYOUT_KEY).toggleScenario();
  },
  body: ScenarioProgress,
  badge: () => {
    const live = getFeature<RunsApi>(RUNS_KEY).getSnapshot().live;
    if (!live?.scenario) return null;
    return live.scenario.totalScripts;
  },
});

// Auto-open the Scenario pane the first time a scenario starts so
// the user sees per-script progress without hunting for the stripe.
// Subscription is deferred to the next microtask — at module load
// the feature registry has not yet been populated by main.tsx, and
// getFeature throws when called before registration.
let lastScenarioRunId: string | null = null;
queueMicrotask(() => {
  getFeature<RunsApi>(RUNS_KEY).subscribe(() => {
    const live = getFeature<RunsApi>(RUNS_KEY).getSnapshot().live;
    if (!live?.scenario) return;
    if (live.runId === lastScenarioRunId) return;
    lastScenarioRunId = live.runId;
    getFeature<LayoutApi>(LAYOUT_KEY).setBottomPane("scenario");
    getFeature<LayoutApi>(LAYOUT_KEY).setProblems(true);
  });
});
