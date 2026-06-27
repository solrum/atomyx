import { createElement } from "react";
import { ListTree } from "lucide-react";
import { getFeature } from "../../../../state/core/registry.js";
import { toolWindowRegistry } from "../../../shell/tool-window-registry.js";
import type { LayoutApi } from "../../../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../../../state/features/layout/index.js";

toolWindowRegistry.register({
  id: "structure",
  side: "left",
  icon: createElement(ListTree, { className: "h-3.5 w-3.5" }),
  label: "Structure",
  isVisible: () => getFeature<LayoutApi>(LAYOUT_KEY).getSnapshot().structureVisible,
  toggle: () => getFeature<LayoutApi>(LAYOUT_KEY).toggleStructure(),
});
