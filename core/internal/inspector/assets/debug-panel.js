import { ApiError } from './api.js';
import { on } from './bus.js';
import { addPanel, describeError, el } from './panel-dom.js';

const PAUSE = '/api/v1/debug/pause';
const RESUME = '/api/v1/debug/resume';
const STEP = '/api/v1/debug/step';
const BREAKPOINTS = '/api/v1/debug/breakpoints';
const UNSUPPORTED_CODE = 'UNSUPPORTED';
// The panel shows while a run is going or stopped, because those are the
// only states in which a debug control has anything to act on.
const RUN_STATES = new Set(['running', 'paused']);

function button(label, onClick) {
  const b = el('button', null, label);
  b.type = 'button';
  b.addEventListener('click', onClick);
  return b;
}

function locationText(pause) {
  if (!pause) {
    return 'The run is paused.';
  }
  const where = pause.line > 0 ? `${pause.source}:${pause.line}` : pause.source;
  return `Paused before ${pause.command} at ${where}, depth ${pause.depth}.`;
}

// Pause, resume and step controls for the run the holder drives, the
// place it is paused at, and a breakpoint list. The list is kept by the
// page and sent whole on every change, because the holder replaces its
// breakpoints with each list it receives.
export function createDebugPanel(host, { operate }) {
  const body = addPanel(host, 'Debugger');
  const section = body.parentElement;
  section.hidden = true;
  const where = el('p', null, '');
  const pause = button('Pause', () => send(PAUSE, {}));
  const resume = button('Resume', () => send(RESUME, { force: false }));
  const step = button('Step', () => send(STEP, { force: false }));
  const controls = el('div', 'yaml-controls');
  controls.append(pause, resume, step);
  const miss = el('div', 'note');
  miss.hidden = true;
  const missText = el('span');
  const anyway = button('Resume anyway', () => send(missPath, { force: true }));
  miss.append(missText, ' ', anyway);
  const message = el('p', 'note');
  message.hidden = true;
  const list = el('ul', 'candidates');
  const location = el('input');
  location.type = 'text';
  location.placeholder = 'script.yaml:12';
  location.autocomplete = 'off';
  location.setAttribute('aria-label', 'Breakpoint location');
  const add = button('Add breakpoint', () => addBreakpoint());
  const breakpointRow = el('div', 'yaml-controls');
  breakpointRow.append(location, add);
  body.append(where, controls, miss, message, el('h3', 'panel-subtitle', 'Breakpoints'), list, breakpointRow);

  let breakpoints = [];
  let missPath = RESUME;
  let state = '';
  let unsupported = false;

  function say(text) {
    message.textContent = text;
    message.hidden = text === '';
  }

  function clearMiss() {
    miss.hidden = true;
  }

  function renderBreakpoints() {
    list.replaceChildren(...breakpoints.map((loc) => {
      const row = el('li', 'candidate-row');
      row.append(el('span', null, loc), ' ', button('Remove', () => applyBreakpoints(breakpoints.filter((b) => b !== loc))));
      return row;
    }));
  }

  function update(info) {
    state = info.state;
    section.hidden = unsupported || !RUN_STATES.has(state);
    const paused = state === 'paused';
    pause.disabled = state !== 'running';
    resume.disabled = !paused;
    step.disabled = !paused;
    where.textContent = paused ? locationText(info.pause) : 'The run is going. Pause it to stop before the next step.';
    if (!paused) {
      clearMiss();
    }
  }

  function fail(err) {
    if (err instanceof ApiError && err.code === UNSUPPORTED_CODE) {
      unsupported = true;
      section.hidden = true;
      return;
    }
    say(describeError(err));
  }

  async function send(path, request) {
    say('');
    clearMiss();
    try {
      const out = await operate(path, request);
      if (out.resumed === false) {
        missPath = path;
        missText.textContent = `The next step may not find its target: ${out.missReason || 'no reason was given'}.`;
        miss.hidden = false;
      }
    } catch (err) {
      fail(err);
    }
  }

  async function applyBreakpoints(next) {
    say('');
    try {
      await operate(BREAKPOINTS, { locations: next });
      breakpoints = next;
      renderBreakpoints();
    } catch (err) {
      fail(err);
    }
  }

  async function addBreakpoint() {
    const value = location.value.trim();
    if (value === '' || breakpoints.includes(value)) {
      say(value === '' ? 'Type a location such as script.yaml:12.' : 'That breakpoint is already set.');
      return;
    }
    await applyBreakpoints([...breakpoints, value]);
    location.value = '';
  }

  on('session', update);
}
