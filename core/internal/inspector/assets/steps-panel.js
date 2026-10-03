import { addPanel, el } from './panel-dom.js';

// How many step lines the list keeps. Older lines scroll out, because
// the list only shows what the run just did.
const MAX_STEPS = 50;

const STARTED_PHASE = 'started';

// A started line has no outcome yet, so it reads "started". A completed
// line reads "ok" or "failed" with its duration.
function lineText(step) {
  if (step.phase === STARTED_PHASE) {
    return `${step.command} started`;
  }
  const outcome = step.ok ? 'ok' : 'failed';
  return `${step.command} ${outcome} ${step.durationMs} ms`;
}

function lineClass(step) {
  return step.phase !== STARTED_PHASE && !step.ok ? 'step step-failed' : 'step';
}

// Lists the step lines of the run the holder drives, newest last. The
// completed line of a step updates the row its started line made. Lines
// of a new run replace the list, and a gap notice says lines were lost.
// Every value is written with textContent, never as markup.
export function createStepsPanel(host) {
  const body = addPanel(host, 'Steps');
  const list = el('ol', 'steps');
  const empty = el('p', 'muted', 'No run is in progress.');
  const heading = el('p', 'note', '');
  heading.hidden = true;
  body.append(empty, heading, list);
  let runId = null;
  let rows = new Map();

  function show(text) {
    heading.textContent = text;
    heading.hidden = text === '';
  }

  return {
    add(step) {
      if (step.runId !== runId) {
        runId = step.runId;
        list.replaceChildren();
        rows = new Map();
        show('');
      }
      empty.hidden = true;
      let item = rows.get(step.stepIndex);
      if (!item) {
        item = el('li', 'step', '');
        rows.set(step.stepIndex, item);
        list.append(item);
      }
      item.className = lineClass(step);
      item.textContent = lineText(step);
      item.title = step.detail || '';
      while (list.children.length > MAX_STEPS) {
        list.firstChild.remove();
      }
    },
    gap() {
      empty.hidden = true;
      show('Some earlier steps were not received.');
    },
  };
}
