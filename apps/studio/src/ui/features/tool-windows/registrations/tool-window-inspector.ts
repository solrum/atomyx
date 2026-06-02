import { createElement } from "react";
import { Crosshair } from "lucide-react";
import { getFeature } from "../../../../state/core/registry.js";
import { toolWindowRegistry } from "../../../shell/tool-window-registry.js";
import type { MirrorWindowApi } from "../../../../state/features/mirror-window/index.js";
import { MIRROR_WINDOW_KEY } from "../../../../state/features/mirror-window/index.js";
import type { UiInspectorApi } from "../../../../state/features/ui-inspector/index.js";
import { UI_INSPECTOR_KEY } from "../../../../state/features/ui-inspector/index.js";

// Right stripe entry for the UI inspector. Inspector lives inside
// the mirror window's `inspector` mode, so toggling here flips the
// mirror's mode + ensures it is open. Turning the inspector off
// drops any pinned node so the picker starts fresh next time.
toolWindowRegistry.register({
  id: "inspector",
  side: "right",
  icon: createElement(Crosshair, { className: "h-3.5 w-3.5" }),
  label: "Inspector",
  isVisible: () => {
    const s = getFeature<MirrorWindowApi>(MIRROR_WINDOW_KEY).getSnapshot();
    return s.isOpen && s.mode === "inspector";
  },
  toggle: () => {
    const api = getFeature<MirrorWindowApi>(MIRROR_WINDOW_KEY);
    const s = api.getSnapshot();
    if (s.isOpen && s.mode === "inspector") {
      api.setMode("compact");
      getFeature<UiInspectorApi>(UI_INSPECTOR_KEY).clear();
    } else {
      api.setMode("inspector");
      if (!s.isOpen) {
        api.setDock("right");
        api.open();
      }
    }
  },
});
