import { createElement } from "react";
import { Files } from "lucide-react";
import { getFeature } from "../../../state/core/registry.js";
import { toolWindowRegistry } from "../../shell/tool-window-registry.js";
import type { LayoutApi } from "../../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../../state/features/layout/index.js";

toolWindowRegistry.register({
  id: "projects",
  side: "left",
  icon: createElement(Files, { className: "h-3.5 w-3.5" }),
  label: "Project",
  isVisible: () => getFeature<LayoutApi>(LAYOUT_KEY).getSnapshot().fileTreeVisible,
  toggle: () => getFeature<LayoutApi>(LAYOUT_KEY).toggleFileTree(),
});
