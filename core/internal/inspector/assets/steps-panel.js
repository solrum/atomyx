import { addPanel, el } from './panel-dom.js';

// How many step lines the list keeps. Older lines scroll out, because
// the list only shows what the run just did.
const MAX_STEPS = 50;

function lineText(step) {
  const outcome = step.ok ? 'ok' : 'failed';
  return `${step.seq}. ${step.command} ${step.phase} ${outcome} ${step.durationMs} ms`;
}

// Lists the step lines of the run the holder drives, newest last. Lines
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

  function show(text) {
    heading.textContent = text;
    heading.hidden = text === '';
  }

  return {
    add(step) {
      if (step.runId !== runId) {
        runId = step.runId;
        list.replaceChildren();
        show('');
      }
      empty.hidden = true;
      const item = el('li', step.ok ? 'step' : 'step step-failed', lineText(step));
      if (step.detail) {
        item.title = step.detail;
      }
      list.append(item);
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
