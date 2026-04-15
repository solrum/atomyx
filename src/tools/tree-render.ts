import type { CompactElement } from "../adapters/device-controller.port.js";

/**
 * Single line for a tree element. Uses explicit key="value" form for the
 * primary selector so the agent can copy-paste the value verbatim into a
 * selector argument — including any slashes / dots / unicode in the id.
 *
 * Format:
 *   resourceId="..." [role] ["label"] [○] [@cx,cy]
 *   contentDesc="..." [...]
 *   text="..." [...]
 *
 * Quotes prevent ambiguity when ids contain `/` (e.g. `G01-05-01/3` is ONE
 * id, not a path). Agent sees the value between quotes and copies it whole.
 */
export function renderCompactLine(
  e: CompactElement,
  selectorDupCount?: number,
): string {
  const sel = e.selector ?? {};
  let token: string;
  let label = e.label ?? "";
  if (sel.resourceId) {
    token = `resourceId="${sel.resourceId}"`;
  } else if (sel.contentDesc) {
    token = `contentDesc="${sel.contentDesc}"`;
    if (label === sel.contentDesc) label = "";
  } else if (sel.text) {
    token = `text="${sel.text}"`;
    if (label === sel.text) label = "";
  } else {
    token = e.role;
  }
  const roleStr = e.role && e.role !== "view" && sel.resourceId ? ` ${e.role}` : "";
  const labelStr = label ? ` "${label.slice(0, 60)}"` : "";
  // Note: NO clickable indicator. The `clickable` flag is unreliable on
  // Flutter / Compose / RN — gestures are dispatched in-engine without
  // setting a11y flags. Agents must NOT use clickable to decide whether to tap.
  const cx = e.bounds ? Math.round((e.bounds.left + e.bounds.right) / 2) : 0;
  const cy = e.bounds ? Math.round((e.bounds.top + e.bounds.bottom) / 2) : 0;
  const coordStr = e.bounds ? ` @${cx},${cy}` : "";
  // Ambiguity marker: if multiple elements share the same selector token,
  // the selector alone is NOT enough — agent must disambiguate by coords or nth.
  const dupStr = selectorDupCount && selectorDupCount > 1 ? ` (${selectorDupCount}×)` : "";
  return `${token}${roleStr}${labelStr}${coordStr}${dupStr}`;
}

/**
 * Sort elements by selector stability rank (resourceId > contentDesc > text >
 * none), then by reading order (top-left).
 */
export function sortByStability(elements: CompactElement[]): CompactElement[] {
  const rank = (e: CompactElement) => {
    if (e.selector?.resourceId) return 0;
    if (e.selector?.contentDesc) return 1;
    if (e.selector?.text) return 2;
    return 3;
  };
  return [...elements].sort((a, b) => {
    const ra = rank(a);
    const rb = rank(b);
    if (ra !== rb) return ra - rb;
    const ta = a.bounds?.top ?? 0;
    const tb = b.bounds?.top ?? 0;
    if (ta !== tb) return ta - tb;
    return (a.bounds?.left ?? 0) - (b.bounds?.left ?? 0);
  });
}

/**
 * Filter to only elements addressable by a stable selector.
 */
export function filterStable(elements: CompactElement[]): CompactElement[] {
  return elements.filter(
    (e) => e.selector?.resourceId || e.selector?.contentDesc || e.selector?.text,
  );
}
