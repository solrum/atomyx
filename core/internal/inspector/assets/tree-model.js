import { containsPoint, parseBounds } from './geometry.js';

export const DEFAULT_EXPAND_DEPTH = 2;

const SEARCH_FIELDS = ['id', 'label', 'text', 'hint', 'role'];
const FIELD_SEPARATOR = '\u0000';

function entryFor(node, path, depth) {
  const attrs = node.attributes || {};
  return {
    node,
    path,
    key: path.join('.'),
    depth,
    children: [],
    haystack: SEARCH_FIELDS.map((f) => attrs[f] || '').join(FIELD_SEPARATOR).toLowerCase(),
  };
}

// The tree as rows can use it. Every node becomes an entry holding its
// path, its depth and a lowercase search string, built once per tree so
// a keystroke in the search box costs one scan and no allocation.
export function buildEntries(root) {
  const top = entryFor(root, [], 0);
  const stack = [top];
  while (stack.length > 0) {
    const entry = stack.pop();
    const kids = entry.node.children || [];
    for (let i = 0; i < kids.length; i += 1) {
      const child = entryFor(kids[i], entry.path.concat(i), entry.depth + 1);
      child.parent = entry;
      entry.children.push(child);
      stack.push(child);
    }
  }
  return top;
}

// Entries whose search string holds query, in document order, and the
// set of their ancestors, which is all a search needs to open.
export function search(top, query) {
  const needle = query.trim().toLowerCase();
  const matches = [];
  const openPaths = new Set();
  if (needle === '') {
    return { matches, openPaths };
  }
  const stack = [top];
  while (stack.length > 0) {
    const entry = stack.pop();
    if (entry.haystack.includes(needle)) {
      matches.push(entry);
      for (let up = entry.parent; up && !openPaths.has(up); up = up.parent) {
        openPaths.add(up);
      }
    }
    for (let i = entry.children.length - 1; i >= 0; i -= 1) {
      stack.push(entry.children[i]);
    }
  }
  return { matches, openPaths };
}

// The rows to show, in order: every entry whose ancestors are all open.
// An entry is open by the operator's choice when there is one, else
// because a search runs through it, else because it is shallow.
export function visibleRows(top, state) {
  const rows = [];
  const stack = [top];
  while (stack.length > 0) {
    const entry = stack.pop();
    rows.push(entry);
    if (entry.children.length > 0 && isOpenEntry(entry, state)) {
      for (let i = entry.children.length - 1; i >= 0; i -= 1) {
        stack.push(entry.children[i]);
      }
    }
  }
  return rows;
}

export function isOpenEntry(entry, { overrides, openPaths }) {
  if (overrides.has(entry.key)) {
    return overrides.get(entry.key);
  }
  return openPaths ? openPaths.has(entry) : entry.depth < DEFAULT_EXPAND_DEPTH;
}

// The row label. A secure node's text and value are masks, so its row
// names it by the hint or label that tells which field it is.
export function displayName(node) {
  const attrs = node.attributes || {};
  if (attrs.secure === 'true') {
    return attrs.hint || attrs.label || attrs.id || '';
  }
  return attrs.id || attrs.label || attrs.text || attrs.hint || '';
}

export function displayRole(node) {
  return (node.attributes || {}).role || 'node';
}

// The deepest entry whose bounds contain the point, in the device's own
// units. Among entries at equal depth the later one wins, because a
// later sibling is drawn over an earlier one.
export function deepestAt(top, x, y) {
  let best = null;
  const stack = [top];
  while (stack.length > 0) {
    const entry = stack.pop();
    const bounds = parseBounds((entry.node.attributes || {}).bounds);
    if (bounds && containsPoint(bounds, x, y) && (best === null || entry.depth >= best.depth)) {
      best = entry;
    }
    for (let i = entry.children.length - 1; i >= 0; i -= 1) {
      stack.push(entry.children[i]);
    }
  }
  return best;
}

export function entryAtPath(top, path) {
  let entry = top;
  for (const index of path) {
    entry = entry.children[index];
    if (!entry) {
      return null;
    }
  }
  return entry;
}
