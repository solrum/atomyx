import { useState, useRef, useEffect } from "react";
import type { ReactElement, ReactNode } from "react";

const SHOW_DELAY_MS = 450;
const HIDE_DELAY_MS = 120;

/**
 * Minimal tooltip with IntelliJ-style shortcut rendering. Accepts
 * a single-child trigger and a label + optional `shortcut`
 * string; both appear stacked when the tooltip shows. Delay
 * values mirror IntelliJ: slow to appear, fast to dismiss.
 */
export interface TooltipProps {
  readonly label: string;
  readonly shortcut?: string;
  readonly children: ReactElement;
}

export function Tooltip({ label, shortcut, children }: TooltipProps) {
  const [visible, setVisible] = useState(false);
  const [position, setPosition] = useState<{ top: number; left: number } | null>(
    null,
  );
  const showTimeoutRef = useRef<number | null>(null);
  const hideTimeoutRef = useRef<number | null>(null);
  const triggerRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    return () => {
      if (showTimeoutRef.current) window.clearTimeout(showTimeoutRef.current);
      if (hideTimeoutRef.current) window.clearTimeout(hideTimeoutRef.current);
    };
  }, []);

  const onEnter = (e: React.MouseEvent<HTMLElement>) => {
    const el = e.currentTarget;
    triggerRef.current = el;
    if (hideTimeoutRef.current) {
      window.clearTimeout(hideTimeoutRef.current);
      hideTimeoutRef.current = null;
    }
    showTimeoutRef.current = window.setTimeout(() => {
      const rect = el.getBoundingClientRect();
      setPosition({
        top: rect.bottom + 6,
        left: rect.left + rect.width / 2,
      });
      setVisible(true);
    }, SHOW_DELAY_MS);
  };

  const onLeave = () => {
    if (showTimeoutRef.current) {
      window.clearTimeout(showTimeoutRef.current);
      showTimeoutRef.current = null;
    }
    hideTimeoutRef.current = window.setTimeout(() => {
      setVisible(false);
    }, HIDE_DELAY_MS);
  };

  const child = children as ReactElement<{
    onMouseEnter?: (e: React.MouseEvent<HTMLElement>) => void;
    onMouseLeave?: () => void;
    onFocus?: (e: React.FocusEvent<HTMLElement>) => void;
    onBlur?: () => void;
  }>;

  const trigger = Object.assign({}, child.props, {
    onMouseEnter: onEnter,
    onMouseLeave: onLeave,
    onFocus: onEnter as unknown as (e: React.FocusEvent<HTMLElement>) => void,
    onBlur: onLeave,
  });

  return (
    <>
      {wrapWithProps(child, trigger)}
      {visible && position ? (
        <div
          className="fixed z-50 px-2 py-1 rounded text-xs shadow-lg pointer-events-none"
          style={{
            top: position.top,
            left: position.left,
            transform: "translateX(-50%)",
            background: "var(--bg-hover)",
            color: "var(--fg-0)",
            border: "1px solid var(--line)",
            maxWidth: 240,
          }}
          role="tooltip"
        >
          <div>{label}</div>
          {shortcut ? (
            <div
              className="mt-0.5 font-mono text-[11px]"
              style={{ color: "var(--fg-2)" }}
            >
              {shortcut}
            </div>
          ) : null}
        </div>
      ) : null}
    </>
  );
}

function wrapWithProps(element: ReactElement, props: Record<string, unknown>): ReactNode {
  return cloneMinimal(element, props);
}

/**
 * Tiny clone — React.cloneElement would also work but importing
 * React's namespace just for this one call bloats bundles in older
 * setups. Stays identical in behavior for the narrow trigger use
 * case here.
 */
function cloneMinimal(
  element: ReactElement,
  extraProps: Record<string, unknown>,
): ReactElement {
  const type = element.type;
  const mergedProps = { ...element.props, ...extraProps };
  return {
    ...element,
    type,
    props: mergedProps,
  } as ReactElement;
}
