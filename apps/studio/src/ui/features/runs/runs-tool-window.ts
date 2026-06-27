import { createElement } from "react";
import { History } from "lucide-react";
import { getFeature } from "../../../state/core/registry.js";
import { toolWindowRegistry } from "../../shell/tool-window-registry.js";
import type { LayoutApi } from "../../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../../state/features/layout/index.js";
import type { RunsApi } from "../../../state/features/runs/index.js";
import { RUNS_KEY } from "../../../state/features/runs/index.js";
import { RunsHistory } from "./runs-history.js";

toolWindowRegistry.register({
  id: "history",
  side: "bottom",
  icon: createElement(History, { className: "h-3.5 w-3.5" }),
  label: "History",
  isVisible: () => {
    const s = getFeature<LayoutApi>(LAYOUT_KEY).getSnapshot();
    return s.problemsVisible && s.bottomPane === "history";
  },
  toggle: () => {
    getFeature<LayoutApi>(LAYOUT_KEY).toggleHistory();
    void getFeature<RunsApi>(RUNS_KEY).loadHistory();
  },
  body: RunsHistory,
  badge: () => {
    const n = getFeature<RunsApi>(RUNS_KEY).getSnapshot().history.length;
    return n > 0 ? n : null;
  },
});
