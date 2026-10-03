import { ApiError } from './api.js';
import { on } from './bus.js';
import { addPanel, describeError, el, summarizeSelector } from './panel-dom.js';
import { sameCapture } from './tree-model.js';

const STALE_CODE = 'TREE_STALE';

function badge(text, kind) {
  return el('span', `badge badge-${kind}`, text);
}

function matchText(count) {
  return count === 1 ? '1 match' : `${count} matches`;
}

// Lists ranked selector candidates for the selected node. The request
// carries the generation and revision of the tree the path was read
// from, and the
// server decides whether each candidate is unique and whether the node
// is obscured. A candidate button hands its selector to onUse.
export function createSuggestionsPanel(host, { operate, notify, reloadTree, onUse }) {
  const body = addPanel(host, 'Selector suggestions');
  let shownTree = null;
  let asked = 0;

  function say(text, className = 'muted') {
    body.replaceChildren(el('p', className, text));
  }

  function candidateRow(candidate, obscured) {
    const button = el('button', 'candidate', summarizeSelector(candidate.selector));
    button.type = 'button';
    button.title = 'Use this selector in the tester';
    button.addEventListener('click', () => onUse(candidate.selector));
    const marks = el('span', 'badges');
    marks.append(
      candidate.unique ? badge('unique', 'ok') : badge('ambiguous', 'warn'),
      badge(matchText(candidate.matchCount), 'plain'),
    );
    if (obscured) {
      marks.append(badge('obscured', 'warn'));
    }
    const item = el('li', 'candidate-row');
    item.append(button, marks);
    return item;
  }

  async function suggest(selection) {
    asked += 1;
    const mine = asked;
    say('Loading suggestions.');
    try {
      const out = await operate('/api/v1/selector/suggest', {
        generation: selection.generation, treeRevision: selection.revision, path: selection.path,
      });
      if (mine !== asked) {
        return;
      }
      if (out.candidates.length === 0) {
        say('The server has no selector to suggest for this node.');
        return;
      }
      const list = el('ul', 'candidates');
      list.append(...out.candidates.map((c) => candidateRow(c, c.obscured)));
      body.replaceChildren(list);
    } catch (err) {
      if (mine !== asked) {
        return;
      }
      if (err instanceof ApiError && err.code === STALE_CODE) {
        notify(describeError(err));
        reloadTree();
        say('The tree changed. Select the node again.');
      } else {
        say(describeError(err), 'note');
      }
    }
  }

  say('Select a node in the tree.');
  on('tree', (response) => {
    shownTree = { generation: response.generation, revision: response.revision };
  });
  on('selection', (selection) => {
    if (shownTree !== null && !sameCapture(selection, shownTree)) {
      asked += 1;
      say('This node comes from an earlier capture. Select it again in the current tree.');
      return;
    }
    suggest(selection);
  });
}
