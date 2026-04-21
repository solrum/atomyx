/**
 * Axis-aligned rectangle in logical points (not pixels).
 *
 * Stored on {@link TreeNode.attributes} as the string "l,t,r,b" under
 * the {@link AttrKeys.Bounds} key. Keeping geometry serialized as a
 * string lets the attribute bag stay flat and JSON-friendly without
 * forcing a typed field on every node.
 */
export interface Bounds {
  readonly left: number;
  readonly top: number;
  readonly right: number;
  readonly bottom: number;
}

export function parseBounds(s: string | undefined): Bounds | null {
  if (!s) return null;
  const parts = s.split(",");
  if (parts.length !== 4) return null;
  const [l, t, r, b] = parts.map((p) => Number(p.trim()));
  if (!Number.isFinite(l) || !Number.isFinite(t)) return null;
  if (!Number.isFinite(r) || !Number.isFinite(b)) return null;
  return { left: l, top: t, right: r, bottom: b };
}

export function formatBounds(b: Bounds): string {
  return `${b.left},${b.top},${b.right},${b.bottom}`;
}

export function boundsCenter(b: Bounds): { x: number; y: number } {
  return {
    x: (b.left + b.right) / 2,
    y: (b.top + b.bottom) / 2,
  };
}

export function boundsContain(b: Bounds, x: number, y: number): boolean {
  return x >= b.left && x < b.right && y >= b.top && y < b.bottom;
}

export function boundsIntersect(a: Bounds, b: Bounds): boolean {
  return !(a.right <= b.left || b.right <= a.left || a.bottom <= b.top || b.bottom <= a.top);
}

export function boundsWidth(b: Bounds): number {
  return b.right - b.left;
}

export function boundsHeight(b: Bounds): number {
  return b.bottom - b.top;
}
