// Parses a node's bounds attribute, "left,top,right,bottom" in the
// device's own units, or returns null when the node reports none.
export function parseBounds(text) {
  const parts = String(text || '').split(',');
  if (parts.length !== 4 || parts.some((part) => part.trim() === '')) {
    return null;
  }
  const [left, top, right, bottom] = parts.map(Number);
  if (![left, top, right, bottom].every(Number.isFinite)) {
    return null;
  }
  return { left, top, right, bottom };
}

export function containsPoint(bounds, x, y) {
  return x >= bounds.left && x < bounds.right && y >= bounds.top && y < bounds.bottom;
}
