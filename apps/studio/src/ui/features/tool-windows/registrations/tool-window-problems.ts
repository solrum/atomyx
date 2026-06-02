import { createElement } from "react";
import { AlertCircle } from "lucide-react";
import { getFeature } from "../../../../state/core/registry.js";
import { toolWindowRegistry } from "../../../shell/tool-window-registry.js";
import type { LayoutApi } from "../../../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../../../state/features/layout/index.js";
import type { ProblemsApi } from "../../../../state/features/problems/index.js";
import { PROBLEMS_KEY, problemCounts } from "../../../../state/features/problems/index.js";
import { ProblemsList } from "../problems-list.js";

toolWindowRegistry.register({
  id: "problems",
  side: "bottom",
  icon: createElement(AlertCircle, { className: "h-3.5 w-3.5" }),
  label: "Problems",
  isVisible: () => {
    const s = getFeature<LayoutApi>(LAYOUT_KEY).getSnapshot();
    return s.problemsVisible && s.bottomPane === "problems";
  },
  toggle: () => getFeature<LayoutApi>(LAYOUT_KEY).toggleProblems(),
  body: ProblemsList,
  badge: () => {
    const c = problemCounts(getFeature<ProblemsApi>(PROBLEMS_KEY).getSnapshot().items);
    const n = c.errors + c.warnings;
    return n > 0 ? n : null;
  },
});
