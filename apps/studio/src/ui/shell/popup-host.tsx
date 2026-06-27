import { useEffect } from "react";
import { getFeature } from "../../state/core/registry.js";
import type { PopupsApi } from "../../state/features/popups/index.js";
import { POPUPS_KEY, usePopups } from "../../state/features/popups/index.js";
import { popupRegistry } from "./popup-registry.js";

/**
 * Renders every registered popup, toggling its `open` prop based
 * on the popups feature snapshot. Escape closes all open popups
 * in one pass — popup components never own visibility state.
 */
export function PopupHost() {
  const { isOpen } = usePopups();

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") getFeature<PopupsApi>(POPUPS_KEY).closeAll();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <>
      {popupRegistry.all().map(({ id, Component }) => (
        <Component
          key={id}
          open={isOpen(id)}
          onClose={() => getFeature<PopupsApi>(POPUPS_KEY).close(id)}
        />
      ))}
    </>
  );
}
