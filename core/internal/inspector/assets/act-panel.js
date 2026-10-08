import { on } from './bus.js';
import { addPanel, describeError, el } from './panel-dom.js';

const ACTIONS = ['tap', 'type'];
const TYPE_ACTION = 'type';

// Runs one step on the device while the run is paused, using the
// selector in the tester. The server renders the step, then the holder
// runs it and reports the outcome. The text to type is a password input
// so it is not shown, it is cleared after every run, and the rendered
// step is never displayed.
export function createActPanel(host, { operate, readSelector, onActed }) {
  const body = addPanel(host, 'Run step');
  const section = body.parentElement;
  section.hidden = true;
  const action = el('select');
  action.setAttribute('aria-label', 'Step action');
  action.append(...ACTIONS.map((name) => {
    const option = el('option', null, name);
    option.value = name;
    return option;
  }));
  const text = el('input');
  text.type = 'password';
  text.placeholder = 'Text to type';
  text.autocomplete = 'off';
  text.setAttribute('aria-label', 'Text to type');
  text.hidden = true;
  const run = el('button', null, 'Run step');
  run.type = 'button';
  const result = el('p', 'note');
  result.hidden = true;
  const controls = el('div', 'yaml-controls');
  controls.append(action, text, run);
  body.append(el('p', 'muted', 'Runs on the device with the selector in the tester. It runs once and does not change the variables of the paused run.'), controls, result);

  function show(message) {
    result.textContent = message;
    result.hidden = false;
  }

  async function runStep() {
    let selector;
    try {
      selector = readSelector();
    } catch (err) {
      show(err.message);
      return;
    }
    const request = { selector, action: action.value };
    if (action.value === TYPE_ACTION) {
      request.text = text.value;
    }
    run.disabled = true;
    try {
      const rendered = await operate('/api/v1/step/yaml', request);
      const out = await operate('/api/v1/act', { step: rendered.yaml });
      const detail = out.message ? ` ${out.message}` : '';
      show(`${out.command} ${out.outcome} in ${out.durationMs} ms.${detail}`);
      onActed();
    } catch (err) {
      show(describeError(err));
    } finally {
      text.value = '';
      run.disabled = false;
    }
  }

  action.addEventListener('change', () => {
    text.hidden = action.value !== TYPE_ACTION;
  });
  run.addEventListener('click', runStep);
  on('session', (info) => {
    section.hidden = info.state !== 'paused';
  });
}
