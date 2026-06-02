import { useEffect, useRef } from "react";

export interface ResizeHandleProps {
  readonly orientation: "vertical" | "horizontal";
  readonly value: number;
  readonly onChange: (value: number) => void;
  readonly onReset?: () => void;
  /**
   * When the handle is vertical (column separator), the width grows
   * as the pointer moves right if `invert` is false; true flips the
   * sign — use for the right-side pane where dragging left grows it.
   * For horizontal separators, true means dragging up grows the
   * value (use for the bottom pane).
   */
  readonly invert?: boolean;
}

export function ResizeHandle({
  orientation,
  value,
  onChange,
  onReset,
  invert = false,
}: ResizeHandleProps) {
  const startRef = useRef<{ pos: number; initial: number } | null>(null);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!startRef.current) return;
      const axis = orientation === "vertical" ? e.clientX : e.clientY;
      const delta = axis - startRef.current.pos;
      const next = startRef.current.initial + (invert ? -delta : delta);
      onChange(next);
    };
    const onUp = () => {
      startRef.current = null;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, [orientation, onChange, invert]);

  const onMouseDown = (e: React.MouseEvent) => {
    e.preventDefault();
    const axis = orientation === "vertical" ? e.clientX : e.clientY;
    startRef.current = { pos: axis, initial: value };
    document.body.style.cursor =
      orientation === "vertical" ? "col-resize" : "row-resize";
    document.body.style.userSelect = "none";
  };

  const onDoubleClick = () => onReset?.();

  const style: React.CSSProperties =
    orientation === "vertical"
      ? {
          width: 4,
          height: "100%",
          cursor: "col-resize",
          background: "transparent",
          flex: "0 0 auto",
        }
      : {
          width: "100%",
          height: 4,
          cursor: "row-resize",
          background: "transparent",
          flex: "0 0 auto",
        };

  // Handle is invisible at rest so it doesn't paint a coloured
  // strip next to the pane border. A brief tint on hover signals
  // the drag target; active drag keeps the tint while the cursor
  // moves outside the handle rect.
  return (
    <div
      role="separator"
      aria-orientation={orientation}
      onMouseDown={onMouseDown}
      onDoubleClick={onDoubleClick}
      className="hover:bg-[color:var(--line)] transition-colors"
      style={style}
    />
  );
}
