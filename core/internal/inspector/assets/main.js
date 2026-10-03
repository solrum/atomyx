import { ApiError, DEVICE_UNAVAILABLE_CODES, coalesced, getJson, postJson } from './api.js';
import { bootstrap } from './bootstrap.js';
import {
  hideReleased, showFatal, showNotice, showReleased, unavailableText,
} from './banners.js';
import { createEventStream } from './events.js';
import { createHighlight } from './highlight.js';
import { createPropertiesPanel } from './properties-panel.js';
import { refreshScreenshot } from './screenshot.js';
import { refreshBlocked, refreshSession } from './session-header.js';
import { createStepsPanel } from './steps-panel.js';
import { createSuggestionsPanel } from './suggestions-panel.js';
import { createTesterPanel } from './tester-panel.js';
import { createYamlPanel } from './yaml-panel.js';
import { bindSearch } from './tree-search.js';
import { createTreeView } from './tree-view.js';

const HOLDER_BUSY_CODE = 'HOLDER_BUSY';
const EMPTY_CODES = new Set(['NO_TREE', 'NO_SCREENSHOT']);
// How often the page reads the holder state, which changes without a
// capture event when a run ends or a call finishes.
const SESSION_POLL_MS = 3000;

const byId = (id) => document.getElementById(id);

const view = createTreeView({
  viewport: byId('tree-viewport'),
  rowsEl: byId('tree-rows'),
  spacer: byId('tree-spacer'),
  emptyEl: byId('tree-empty'),
  countEl: byId('search-count'),
});

let events = null;

function handleFailure(err) {
  if (err instanceof ApiError && DEVICE_UNAVAILABLE_CODES.has(err.code)) {
    events?.stop();
    showReleased(unavailableText(err.code));
  } else if (err instanceof ApiError && EMPTY_CODES.has(err.code)) {
    return;
  } else if (err instanceof ApiError && err.status === 401) {
    events?.stop();
    showFatal(err);
  } else {
    showNotice(err.message);
  }
}

function guarded(task) {
  return coalesced(async () => {
    try {
      await task();
    } catch (err) {
      handleFailure(err);
    }
  });
}

const loadSession = guarded(async () => {
  const info = await refreshSession();
  if (info.state === 'lost') {
    events?.stop();
    showReleased(unavailableText('DEVICE_LOST'));
  }
});
const loadScreenshot = guarded(refreshScreenshot);
const loadTree = guarded(async () => {
  view.setTree(await getJson('/api/v1/tree'));
});

// Every operator action ends with a session refetch, because the holder
// state changes while the action runs and no capture event follows it.
// An action refused because the device was released or lost shows the
// reconnect banner, whichever panel took it.
async function operate(path, body) {
  try {
    return await postJson(path, body);
  } catch (err) {
    if (err instanceof ApiError && DEVICE_UNAVAILABLE_CODES.has(err.code)) {
      handleFailure(err);
    }
    throw err;
  } finally {
    loadSession();
  }
}

// Takes the device again on the operator's request, then resumes the
// capture feed and refetches everything the page shows. A refusal, such
// as DEVICE_HELD, keeps the banner up with the server's remedy.
async function reconnect() {
  const button = byId('reconnect');
  button.disabled = true;
  try {
    await operate('/api/v1/reconnect');
    hideReleased();
    events.start();
    loadTree();
    loadScreenshot();
  } catch (err) {
    if (err instanceof ApiError) {
      showReleased(`${err.message} ${err.remedy}`.trim());
    } else {
      showNotice(err.message);
    }
  } finally {
    button.disabled = false;
  }
}

async function refreshCapture() {
  const button = byId('refresh');
  button.disabled = true;
  try {
    view.setTree(await operate('/api/v1/tree/fresh'));
    await loadScreenshot();
  } catch (err) {
    if (err instanceof ApiError && err.code === HOLDER_BUSY_CODE) {
      showNotice(`${err.message} ${err.remedy}`.trim());
    } else {
      handleFailure(err);
    }
  } finally {
    button.disabled = refreshBlocked();
  }
}

async function start() {
  try {
    await bootstrap();
  } catch (err) {
    showFatal(err);
    return;
  }
  byId('layout').hidden = false;
  const panels = byId('panels');
  createPropertiesPanel(panels);
  createSuggestionsPanel(panels, { operate, notify: showNotice, reloadTree: loadTree, onUse: (selector) => tester.setSelector(selector) });
  const tester = createTesterPanel(panels, { operate, notify: showNotice, selectPath: (path) => view.selectPath(path) });
  createYamlPanel(panels, { operate, notify: showNotice, readSelector: tester.readSelector });
  const steps = createStepsPanel(panels);
  createHighlight({
    img: byId('screenshot'),
    overlay: byId('highlight'),
    noteEl: byId('highlight-note'),
    onPick: (x, y) => view.selectAt(x, y),
  });
  bindSearch(byId('search'), view);
  byId('refresh').addEventListener('click', refreshCapture);
  byId('reconnect').addEventListener('click', reconnect);
  events = createEventStream({
    onTree: () => {
      loadTree();
      loadSession();
    },
    onScreenshot: loadScreenshot,
    onStep: steps.add,
    onStepGap: steps.gap,
    onReleased: (code) => {
      showReleased(unavailableText(code));
      loadSession();
    },
    onFatal: showFatal,
  });
  await loadSession();
  events.start();
  setInterval(loadSession, SESSION_POLL_MS);
}

hideReleased();
start();
