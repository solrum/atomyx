import { createElement } from "react";
import { Crosshair } from "lucide-react";
import { getFeature } from "../../../state/core/registry.js";
import { toolWindowRegistry } from "../../shell/tool-window-registry.js";
import type { LayoutApi } from "../../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../../state/features/layout/index.js";

toolWindowRegistry.register({
  id: "inspector",
  side: "right",
  icon: createElement(Crosshair, { className: "h-3.5 w-3.5" }),
  label: "Inspector",
  isVisible: () => getFeature<LayoutApi>(LAYOUT_KEY).getSnapshot().inspectorVisible,
  toggle: () => getFeature<LayoutApi>(LAYOUT_KEY).toggleInspector(),
});
