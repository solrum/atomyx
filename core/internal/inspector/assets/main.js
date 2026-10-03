import { ApiError, coalesced, getJson, postJson } from './api.js';
import { bootstrap } from './bootstrap.js';
import { hideReleased, showFatal, showNotice, showReleased } from './banners.js';
import { createEventStream } from './events.js';
import { refreshScreenshot } from './screenshot.js';
import { refreshSession } from './session-header.js';
import { bindSearch } from './tree-search.js';
import { createTreeView } from './tree-view.js';

const RELEASED_CODE = 'DEVICE_RELEASED';
const HOLDER_BUSY_CODE = 'HOLDER_BUSY';
const EMPTY_CODES = new Set(['NO_TREE', 'NO_SCREENSHOT']);
const RELEASED_TEXT = 'The inspector released the device after it sat idle.';

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
  if (err instanceof ApiError && err.code === RELEASED_CODE) {
    events?.stop();
    showReleased(RELEASED_TEXT);
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

const loadSession = guarded(refreshSession);
const loadScreenshot = guarded(refreshScreenshot);
const loadTree = guarded(async () => {
  view.setTree(await getJson('/api/v1/tree'));
});

async function refreshCapture() {
  const button = byId('refresh');
  button.disabled = true;
  try {
    view.setTree(await postJson('/api/v1/tree/fresh'));
    await loadScreenshot();
  } catch (err) {
    if (err instanceof ApiError && err.code === HOLDER_BUSY_CODE) {
      showNotice(`${err.message} ${err.remedy}`.trim());
    } else {
      handleFailure(err);
    }
  } finally {
    button.disabled = false;
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
  bindSearch(byId('search'), view);
  byId('refresh').addEventListener('click', refreshCapture);
  events = createEventStream({
    onTree: () => {
      loadTree();
      loadSession();
    },
    onScreenshot: loadScreenshot,
    onReleased: () => {
      showReleased(RELEASED_TEXT);
      loadSession();
    },
    onFatal: showFatal,
  });
  await loadSession();
  events.start();
}

hideReleased();
start();
