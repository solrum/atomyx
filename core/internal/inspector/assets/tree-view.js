import { emit } from './bus.js';
import {
  buildEntries, displayName, displayRole, isOpenEntry, search, visibleRows,
} from './tree-model.js';

export const ROW_HEIGHT_PX = 22;
const OVERSCAN_ROWS = 10;
const INDENT_PX = 14;

// An outline of the tree that draws only the rows near the viewport.
// Rows have a fixed height, a spacer gives the scrollbar the full
// length, and the row container is shifted to the first drawn row, so
// the number of elements stays near the height of the viewport divided
// by ROW_HEIGHT_PX plus two overscans, whatever the size of the tree.
export function createTreeView({ viewport, rowsEl, spacer, emptyEl, countEl }) {
  document.documentElement.style.setProperty('--row-height', `${ROW_HEIGHT_PX}px`);

  let top = null;
  let revision = 0;
  let rows = [];
  let overrides = new Map();
  let query = '';
  let found = { matches: [], openPaths: null };
  let matchIndex = -1;
  let selectedKey = null;
  let selectedRevision = null;

  const state = () => ({ overrides, openPaths: query.trim() === '' ? null : found.openPaths });

  function nameWithMarks(text) {
    const span = document.createElement('span');
    span.className = 'tree-name';
    const needle = query.trim().toLowerCase();
    const at = needle === '' ? -1 : text.toLowerCase().indexOf(needle);
    if (at < 0) {
      span.textContent = text;
      return span;
    }
    const mark = document.createElement('mark');
    mark.textContent = text.slice(at, at + needle.length);
    span.append(text.slice(0, at), mark, text.slice(at + needle.length));
    return span;
  }

  function rowElement(entry, index) {
    const el = document.createElement('div');
    el.className = 'tree-row';
    el.setAttribute('role', 'treeitem');
    el.dataset.index = String(index);
    el.style.paddingLeft = `${entry.depth * INDENT_PX}px`;
    if (entry.key === selectedKey && revision === selectedRevision) {
      el.classList.add('selected');
    }
    if (found.matches[matchIndex] === entry) {
      el.classList.add('current-match');
    }
    const twisty = document.createElement('span');
    twisty.className = 'tree-twisty';
    if (entry.children.length > 0) {
      const open = isOpenEntry(entry, state());
      twisty.textContent = open ? '\u25be' : '\u25b8';
      el.setAttribute('aria-expanded', String(open));
    }
    const role = document.createElement('span');
    role.className = 'tree-role';
    role.textContent = displayRole(entry.node);
    el.append(twisty, role, nameWithMarks(displayName(entry.node)));
    return el;
  }

  function draw() {
    const first = Math.max(0, Math.floor(viewport.scrollTop / ROW_HEIGHT_PX) - OVERSCAN_ROWS);
    const last = Math.min(rows.length, Math.ceil((viewport.scrollTop + viewport.clientHeight) / ROW_HEIGHT_PX) + OVERSCAN_ROWS);
    spacer.style.height = `${rows.length * ROW_HEIGHT_PX}px`;
    rowsEl.style.transform = `translateY(${first * ROW_HEIGHT_PX}px)`;
    const drawn = [];
    for (let i = first; i < last; i += 1) {
      drawn.push(rowElement(rows[i], i));
    }
    rowsEl.replaceChildren(...drawn);
  }

  function relayout() {
    rows = top ? visibleRows(top, state()) : [];
    emptyEl.hidden = top !== null;
    draw();
  }

  function scrollToRow(index) {
    const rowTop = index * ROW_HEIGHT_PX;
    if (rowTop < viewport.scrollTop) {
      viewport.scrollTop = rowTop;
    } else if (rowTop + ROW_HEIGHT_PX > viewport.scrollTop + viewport.clientHeight) {
      viewport.scrollTop = rowTop + ROW_HEIGHT_PX - viewport.clientHeight;
    }
  }

  function select(entry) {
    selectedKey = entry.key;
    selectedRevision = revision;
    emit('selection', { path: entry.path, revision, node: entry.node });
    const index = rows.indexOf(entry);
    if (index >= 0) {
      scrollToRow(index);
    }
    draw();
  }

  function toggle(entry) {
    overrides.set(entry.key, !isOpenEntry(entry, state()));
    relayout();
  }

  function countText() {
    if (query.trim() === '') {
      return '';
    }
    return found.matches.length === 0 ? 'No matches' : `${matchIndex + 1} of ${found.matches.length}`;
  }

  viewport.addEventListener('scroll', draw);
  new ResizeObserver(draw).observe(viewport);

  viewport.addEventListener('click', (event) => {
    const el = event.target.closest('.tree-row');
    if (!el) {
      return;
    }
    const entry = rows[Number(el.dataset.index)];
    if (event.target.classList.contains('tree-twisty') && entry.children.length > 0) {
      toggle(entry);
    } else {
      select(entry);
    }
  });

  viewport.addEventListener('keydown', (event) => {
    const current = rows.findIndex((entry) => entry.key === selectedKey);
    const entry = rows[current];
    const moves = { ArrowDown: 1, ArrowUp: -1 };
    if (event.key in moves) {
      const next = rows[Math.min(rows.length - 1, Math.max(0, current + moves[event.key]))];
      if (next) {
        select(next);
      }
    } else if (entry && entry.children.length > 0 && event.key === 'ArrowRight' && !isOpenEntry(entry, state())) {
      toggle(entry);
    } else if (entry && entry.children.length > 0 && event.key === 'ArrowLeft' && isOpenEntry(entry, state())) {
      toggle(entry);
    } else {
      return;
    }
    event.preventDefault();
  });

  return {
    setTree(response) {
      revision = response.revision;
      top = buildEntries(response.tree);
      if (query.trim() !== '') {
        found = search(top, query);
        matchIndex = -1;
      }
      countEl.textContent = countText();
      relayout();
      emit('tree', response);
    },
    setQuery(next) {
      query = next;
      overrides = new Map();
      found = top && query.trim() !== '' ? search(top, query) : { matches: [], openPaths: null };
      matchIndex = -1;
      countEl.textContent = countText();
      relayout();
    },
    stepMatch(direction) {
      if (found.matches.length === 0) {
        return;
      }
      const count = found.matches.length;
      matchIndex = (matchIndex + direction + count) % count;
      countEl.textContent = countText();
      select(found.matches[matchIndex]);
    },
    query() {
      return query;
    },
  };
}
