import { ApiError, getJson } from './api.js';
import { on } from './bus.js';
import { addPanel, describeError, el } from './panel-dom.js';

// The variables the holder reports at the pause. Every value arrives
// already redacted by the holder, and the panel shows it unchanged.
export function createVariablesPanel(host) {
  const body = addPanel(host, 'Variables');
  const section = body.parentElement;
  section.hidden = true;
  const refresh = el('button', null, 'Refresh');
  refresh.type = 'button';
  const note = el('p', 'muted');
  const table = el('table', 'variables');
  body.append(refresh, note, table);
  let shownPause = null;

  function clear(text) {
    table.replaceChildren();
    note.textContent = text;
  }

  async function load() {
    refresh.disabled = true;
    try {
      const out = await getJson('/api/v1/debug/variables');
      const names = Object.keys(out.variables || {}).sort();
      clear(names.length === 0 ? 'No variables are set.' : '');
      table.append(...names.map((name) => {
        const row = el('tr');
        row.append(el('th', null, name), el('td', null, out.variables[name]));
        return row;
      }));
    } catch (err) {
      clear(err instanceof ApiError && err.code === 'HOLDER_BUSY' ? 'The run is not paused.' : describeError(err));
    } finally {
      refresh.disabled = false;
    }
  }

  on('session', (info) => {
    section.hidden = info.state !== 'paused';
    const key = info.state === 'paused' ? JSON.stringify(info.pause || {}) : null;
    if (key !== shownPause) {
      shownPause = key;
      if (key === null) {
        clear('');
      } else {
        load();
      }
    }
  });
  refresh.addEventListener('click', load);
}
