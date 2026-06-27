import { useEffect, useMemo, useState, type RefObject } from "react";

import type { MirrorSessionStatus } from "../../../state/features/mirror/index.js";
import {
  computeTreeExtent,
  resolveUiNode,
  useUiInspector,
} from "../../../state/features/ui-inspector/index.js";
import {
  computeFrameLayout,
  deviceRectToScreenRect,
  parseBoundsAttribute,
  type ScreenRect,
} from "./mirror-geometry.js";

export interface InspectorOverlayProps {
  readonly canvasRef: RefObject<HTMLCanvasElement | null>;
  readonly session: MirrorSessionStatus;
}

/**
 * Renders a translucent rectangle over the mirror canvas to
 * highlight the inspector's selected node. Active only when the
 * selected snapshot was captured for the same device the mirror is
 * streaming; otherwise hidden so a stale tree from another device
 * never paints on top of live pixels.
 */
export function InspectorOverlay({
  canvasRef,
  session,
}: InspectorOverlayProps) {
  const { tree, selectedPath, capturedForDeviceId } = useUiInspector();
  const [rect, setRect] = useState<ScreenRect | null>(null);

  const node = resolveUiNode(tree, selectedPath);
  const deviceMatch =
    capturedForDeviceId !== null && capturedForDeviceId === session.target.id;
  // parseBoundsAttribute returns a fresh object each call; memoise on
  // the raw bounds string so deviceRect keeps a stable reference
  // across renders. Without this the layout effect below (which
  // depends on deviceRect) re-runs every render and setRect loops.
  const boundsAttr = node?.attributes["bounds"] ?? null;
  const deviceRect = useMemo(
    () => (boundsAttr ? parseBoundsAttribute(boundsAttr) : null),
    [boundsAttr],
  );
  const treeExtent = useMemo(() => computeTreeExtent(tree), [tree]);
  const active = Boolean(deviceMatch && deviceRect);

  useEffect(() => {
    if (!active) {
      setRect(null);
      return;
    }
    const canvas = canvasRef.current;
    if (!canvas) return;

    const recompute = () => {
      const c = canvasRef.current;
      if (!c || !deviceRect) return;
      const layout = computeFrameLayout(c, session);
      setRect(
        deviceRectToScreenRect(deviceRect, layout, treeExtent ?? undefined),
      );
    };

    recompute();
    const observer = new ResizeObserver(recompute);
    observer.observe(canvas);
    window.addEventListener("resize", recompute);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", recompute);
    };
  }, [active, deviceRect, treeExtent, session, canvasRef]);

  if (!rect) return null;

  return (
    <div
      aria-hidden
      className="pointer-events-none absolute inset-0"
      style={{ zIndex: 2 }}
    >
      <div
        className="absolute"
        style={{
          left: rect.left,
          top: rect.top,
          width: rect.width,
          height: rect.height,
          border: "1.5px solid var(--accent)",
          background: "rgba(88, 157, 246, 0.16)",
          boxSizing: "border-box",
        }}
      />
    </div>
  );
}
