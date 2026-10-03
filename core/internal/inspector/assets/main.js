import { ApiError, coalesced, postJson } from './api.js';
import { bootstrap } from './bootstrap.js';
import { showFatal, showNotice, showReleased } from './banners.js';
import { createEventStream } from './events.js';
import { refreshScreenshot } from './screenshot.js';
import { refreshSession } from './session-header.js';

const RELEASED_CODE = 'DEVICE_RELEASED';
const HOLDER_BUSY_CODE = 'HOLDER_BUSY';
const EMPTY_CODES = new Set(['NO_TREE', 'NO_SCREENSHOT']);
const RELEASED_TEXT = 'The inspector released the device after it sat idle.';

const byId = (id) => document.getElementById(id);

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

async function refreshCapture() {
  const button = byId('refresh');
  button.disabled = true;
  try {
    await postJson('/api/v1/tree/fresh');
    await Promise.all([loadScreenshot(), loadSession()]);
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
  byId('refresh').addEventListener('click', refreshCapture);
  events = createEventStream({
    onTree: loadSession,
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

start();
