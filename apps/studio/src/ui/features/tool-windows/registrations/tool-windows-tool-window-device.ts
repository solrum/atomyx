import { createElement } from "react";
import { Smartphone } from "lucide-react";
import { getFeature } from "../../../../state/core/registry.js";
import { toolWindowRegistry } from "../../../shell/tool-window-registry.js";
import type { MirrorWindowApi } from "../../../../state/features/mirror-window/index.js";
import { MIRROR_WINDOW_KEY } from "../../../../state/features/mirror-window/index.js";
import type { UiInspectorApi } from "../../../../state/features/ui-inspector/index.js";
import { UI_INSPECTOR_KEY } from "../../../../state/features/ui-inspector/index.js";

// Right stripe entry for the device mirror. Toggling here opens or
// closes the docked mirror slot; closing also clears any pinned
// inspector node so the next session starts without a stale
// selection from the previous device.
toolWindowRegistry.register({
  id: "device",
  side: "right",
  icon: createElement(Smartphone, { className: "h-3.5 w-3.5" }),
  label: "Device",
  isVisible: () => {
    const s = getFeature<MirrorWindowApi>(MIRROR_WINDOW_KEY).getSnapshot();
    return s.isOpen && s.dock === "right";
  },
  toggle: () => {
    const api = getFeature<MirrorWindowApi>(MIRROR_WINDOW_KEY);
    const s = api.getSnapshot();
    if (s.isOpen && s.dock === "right") {
      api.close();
      getFeature<UiInspectorApi>(UI_INSPECTOR_KEY).clear();
    } else {
      api.setDock("right");
      api.open();
    }
  },
});
