import { on } from './bus.js';
import { addPanel, describeError, el, summarizeSelector } from './panel-dom.js';
import { sameCapture } from './tree-model.js';

const EXAMPLE = '{\n  "id": "login_button"\n}';

// A selector editor and a live test against the displayed tree. The
// selector is typed as one JSON object, the same shape a script step
// takes (id, label, text, hint, value, role, enabled, clickable,
// focused, nth), so a pattern written as {"regex": "..."} works too.
// Testing lists the matches in resolution order, and a match button
// selects that node in the tree. The returned handle lets other panels
// read the selector or replace it.
export function createTesterPanel(host, { operate, notify, selectPath }) {
  const body = addPanel(host, 'Selector tester');
  const input = el('textarea', 'selector-input');
  input.rows = 5;
  input.spellcheck = false;
  input.placeholder = EXAMPLE;
  input.setAttribute('aria-label', 'Selector as JSON');
  const run = el('button', null, 'Test selector');
  run.type = 'button';
  const result = el('div', 'tester-result');
  body.append(input, run, result);
  let shownTree = null;

  function readSelector() {
    let parsed;
    try {
      parsed = JSON.parse(input.value);
    } catch {
      throw new Error('The selector is not valid JSON. Write one object, for example {"id": "login_button"}.');
    }
    if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('The selector must be one JSON object.');
    }
    return parsed;
  }

  function matchRow(match, tested) {
    const label = match.path.length === 0 ? 'root' : match.path.join(' / ');
    const button = el('button', 'candidate', label);
    button.type = 'button';
    button.addEventListener('click', () => {
      if (!sameCapture(tested, shownTree) || !selectPath(match.path)) {
        notify('The tree changed since this test. Test the selector again.');
      }
    });
    const item = el('li', 'candidate-row');
    item.append(button);
    if (match.bounds) {
      const b = match.bounds;
      item.append(el('span', 'muted', `${b.left},${b.top},${b.right},${b.bottom}`));
    }
    return item;
  }

  async function test() {
    let selector;
    try {
      selector = readSelector();
    } catch (err) {
      result.replaceChildren(el('p', 'note', err.message));
      return;
    }
    run.disabled = true;
    try {
      const out = await operate('/api/v1/selector/test', { selector });
      const summary = out.count === 0
        ? 'No node matches.'
        : `${out.count} ${out.count === 1 ? 'match' : 'matches'}${out.truncated ? `, first ${out.matches.length} shown` : ''}.`;
      const list = el('ul', 'candidates');
      const tested = { generation: out.generation, revision: out.treeRevision };
      list.append(...out.matches.map((m) => matchRow(m, tested)));
      result.replaceChildren(el('p', null, `${summarizeSelector(selector)}: ${summary}`), list);
    } catch (err) {
      result.replaceChildren(el('p', 'note', describeError(err)));
    } finally {
      run.disabled = false;
    }
  }

  run.addEventListener('click', test);
  on('tree', (response) => {
    shownTree = { generation: response.generation, revision: response.revision };
  });

  return {
    readSelector,
    setSelector(selector) {
      input.value = JSON.stringify(selector, null, 2);
    },
  };
}
