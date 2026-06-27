import { createElement } from "react";
import { ScrollText } from "lucide-react";
import { getFeature } from "../../../../state/core/registry.js";
import { toolWindowRegistry } from "../../../shell/tool-window-registry.js";
import type { LayoutApi } from "../../../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../../../state/features/layout/index.js";
import { LogsList } from "../tool-windows-logs-list.js";

toolWindowRegistry.register({
  id: "logs",
  side: "bottom",
  icon: createElement(ScrollText, { className: "h-3.5 w-3.5" }),
  label: "Logs",
  isVisible: () => {
    const s = getFeature<LayoutApi>(LAYOUT_KEY).getSnapshot();
    return s.problemsVisible && s.bottomPane === "logs";
  },
  toggle: () => getFeature<LayoutApi>(LAYOUT_KEY).toggleLogs(),
  body: LogsList,
});
