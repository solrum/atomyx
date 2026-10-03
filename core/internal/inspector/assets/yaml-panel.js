import { addPanel, describeError, el } from './panel-dom.js';

const ACTIONS = ['tap', 'type', 'waitFor', 'assertVisible', 'assertNotVisible', 'assertExists'];
const TYPE_ACTION = 'type';

// Renders one script step for the tester's selector and an action, and
// copies it to the clipboard. The server renders the YAML. For a secure
// target it replaces the typed text with the placeholder it names, so
// the secret never reaches the page or the clipboard.
export function createYamlPanel(host, { operate, notify, readSelector }) {
  const body = addPanel(host, 'Copy as YAML');
  const action = el('select');
  action.setAttribute('aria-label', 'Step action');
  action.append(...ACTIONS.map((name) => {
    const option = el('option', null, name);
    option.value = name;
    return option;
  }));
  const text = el('input');
  text.type = 'text';
  text.placeholder = 'Text to type';
  text.autocomplete = 'off';
  text.setAttribute('aria-label', 'Text to type');
  text.hidden = true;
  const render = el('button', null, 'Render step');
  render.type = 'button';
  const copy = el('button', null, 'Copy');
  copy.type = 'button';
  copy.hidden = true;
  const note = el('p', 'note');
  note.hidden = true;
  const out = el('pre', 'yaml');
  out.hidden = true;
  const controls = el('div', 'yaml-controls');
  controls.append(action, text, render, copy);
  body.append(el('p', 'muted', 'Uses the selector in the tester.'), controls, note, out);

  function fail(message) {
    out.hidden = true;
    copy.hidden = true;
    note.textContent = message;
    note.hidden = false;
  }

  async function renderStep() {
    let selector;
    try {
      selector = readSelector();
    } catch (err) {
      fail(err.message);
      return;
    }
    const request = { selector, action: action.value };
    if (action.value === TYPE_ACTION) {
      request.text = text.value;
    }
    render.disabled = true;
    try {
      const res = await operate('/api/v1/step/yaml', request);
      out.textContent = res.yaml;
      out.hidden = false;
      copy.hidden = false;
      note.textContent = res.secure
        ? `The target is a secure field, so the step names the placeholder ${res.placeholder} instead of the text. Supply it as an environment variable when the script runs.`
        : '';
      note.hidden = !res.secure;
    } catch (err) {
      fail(describeError(err));
    } finally {
      render.disabled = false;
    }
  }

  async function copyStep() {
    try {
      await navigator.clipboard.writeText(out.textContent);
      notify('Step copied to the clipboard.');
    } catch {
      notify('The browser refused clipboard access. Select the step text and copy it by hand.');
    }
  }

  action.addEventListener('change', () => {
    text.hidden = action.value !== TYPE_ACTION;
  });
  render.addEventListener('click', renderStep);
  copy.addEventListener('click', copyStep);
}
