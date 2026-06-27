import { useEffect } from "react";
import { ChevronDown, Crosshair, PanelRight, X } from "lucide-react";
import { MirrorInspectorPane } from "../features/mirror/index.js";
import { useLayout } from "../../state/features/layout/index.js";
import { useMirrorWindow } from "../../state/features/mirror-window/index.js";
import { useDevices } from "../../state/features/devices/index.js";
import { useUiInspector } from "../../state/features/ui-inspector/index.js";

/**
 * Right-column slot hosting the embedded device mirror + inspector.
 * Header carries the caret + label + device badge + LIVE indicator +
 * chrome buttons (Inspector toggle, Pop-out, Close). Body delegates
 * to `MirrorInspectorPane` so docked + floating placements share the
 * same composition: mirror on the left, inspector tree on the right
 * when active.
 *
 * Pop-out flips `dock` to "free"; the existing `DeviceMirrorWindow`
 * overlay reacts to the same state.
 */
export function MirrorSlot() {
  const win = useMirrorWindow();
  const { devices, selectedId } = useDevices();
  const layout = useLayout();
  const inspector = useUiInspector();
  const { paneSizes } = layout;
  const showInspector = win.mode === "inspector";

  // Mirror-only fits in ~360px; inspector pane needs another ~320px
  // beside it. Widen on first inspector entry; never shrink so a
  // hand-resized column is preserved.
  useEffect(() => {
    if (!showInspector) return;
    const MIN_WITH_INSPECTOR = 640;
    if (paneSizes.mirrorWidth < MIN_WITH_INSPECTOR) {
      layout.setPaneSize("mirrorWidth", MIN_WITH_INSPECTOR);
    }
  }, [showInspector, paneSizes.mirrorWidth, layout]);

  const selected = devices.find((d) => d.id === selectedId) ?? null;
  const liveDot = win.isOpen ? (
    <span style={{ fontSize: 10, color: "var(--ok)" }}>● live</span>
  ) : null;

  return (
    <div className="slot mirror-slot" style={{ flex: "1 1 auto" }}>
      <div className="slot-header">
        <span className="caret" aria-hidden>
          <ChevronDown style={{ width: 10, height: 10 }} />
        </span>
        <span>Device Mirror</span>
        {selected ? (
          <span className="badge-mode">{selected.name}</span>
        ) : null}
        {liveDot}
        <span className="spacer" />
        <button
          type="button"
          className="icon-btn"
          title={showInspector ? "Hide inspector" : "Show inspector"}
          onClick={() => {
            if (showInspector) {
              win.setMode("compact");
              inspector.clear();
            } else {
              win.setMode("inspector");
            }
          }}
        >
          <Crosshair style={{ width: 11, height: 11 }} />
        </button>
        <button
          type="button"
          className="icon-btn"
          title="Pop out"
          onClick={() => win.setDock("free")}
        >
          <PanelRight style={{ width: 11, height: 11 }} />
        </button>
        <button
          type="button"
          className="icon-btn"
          title="Close"
          onClick={() => { win.close(); inspector.clear(); }}
        >
          <X style={{ width: 11, height: 11 }} />
        </button>
      </div>
      <div className="slot-body dmw-embed">
        <MirrorInspectorPane showInspector={showInspector} />
      </div>
    </div>
  );
}
