import { createElement } from "react";
import { TerminalSquare } from "lucide-react";
import { getFeature } from "../../../../state/core/registry.js";
import { toolWindowRegistry } from "../../../shell/tool-window-registry.js";
import type { LayoutApi } from "../../../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../../../state/features/layout/index.js";
import { TerminalView } from "../terminal-view.js";

toolWindowRegistry.register({
  id: "terminal",
  side: "bottom",
  icon: createElement(TerminalSquare, { className: "h-3.5 w-3.5" }),
  label: "Terminal",
  isVisible: () => {
    const s = getFeature<LayoutApi>(LAYOUT_KEY).getSnapshot();
    return s.problemsVisible && s.bottomPane === "terminal";
  },
  toggle: () => getFeature<LayoutApi>(LAYOUT_KEY).toggleTerminal(),
  body: TerminalView,
});
