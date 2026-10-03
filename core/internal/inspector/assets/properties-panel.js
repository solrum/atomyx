import { on } from './bus.js';
import { addPanel, el } from './panel-dom.js';

const STATE_FLAGS = ['clickable', 'enabled', 'focused', 'selected', 'checked', 'visible'];

function row(name, value) {
  const line = el('tr');
  line.append(el('th', null, name), el('td', null, value));
  return line;
}

function table(rows) {
  const t = el('table', 'props');
  t.append(...rows);
  return t;
}

function flagText(value) {
  return value === undefined || value === null ? 'not reported' : String(value);
}

// Shows every attribute of the selected node as the tree response holds
// it, so a secure node's masked values appear masked, with the node's
// path, bounds and state flags. A selection from an earlier tree stays
// readable and says so.
export function createPropertiesPanel(host) {
  const body = addPanel(host, 'Properties');
  let revision = null;

  function showEmpty() {
    body.replaceChildren(el('p', 'muted', 'Select a node in the tree.'));
  }

  function render({ path, revision: selectedRevision, node }) {
    const attrs = node.attributes || {};
    const parts = [];
    if (revision !== null && selectedRevision !== revision) {
      parts.push(el('p', 'note', 'This node comes from an earlier capture.'));
    }
    const names = Object.keys(attrs).sort();
    parts.push(
      table([
        row('path', path.length === 0 ? 'root' : path.join(' / ')),
        row('bounds', attrs.bounds || 'not reported'),
        ...STATE_FLAGS.map((flag) => row(flag, flagText(node[flag]))),
      ]),
      el('h3', 'panel-subtitle', 'Attributes'),
      names.length === 0 ? el('p', 'muted', 'No attributes.') : table(names.map((name) => row(name, attrs[name]))),
    );
    body.replaceChildren(...parts);
  }

  let current = null;
  showEmpty();
  on('selection', (selection) => {
    current = selection;
    render(selection);
  });
  on('tree', (response) => {
    revision = response.revision;
    if (current) {
      render(current);
    }
  });
}
