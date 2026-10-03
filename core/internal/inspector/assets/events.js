import { ApiError, DEVICE_UNAVAILABLE_CODES, request } from './api.js';

const BACKOFF_START_MS = 500;
const BACKOFF_MAX_MS = 8000;
// Consecutive attempts that fail to reach a working feed before the page
// gives up and says the server is gone.
const MAX_CONSECUTIVE_FAILURES = 6;
// Codes a server may answer with while a later attempt can still work.
const RETRYABLE_CODES = new Set(['UNKNOWN']);
const GONE_ERROR = {
  code: 'EVENTS_UNAVAILABLE',
  message: 'The page lost contact with the inspector.',
  remedy: 'Check the terminal that runs atomyx inspect. If it stopped, start it again and open the new URL.',
};

function delay(ms, signal) {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener('abort', () => {
      clearTimeout(timer);
      resolve();
    }, { once: true });
  });
}

async function* ndjson(body, signal) {
  const reader = body.pipeThrough(new TextDecoderStream()).getReader();
  signal.addEventListener('abort', () => reader.cancel(), { once: true });
  let pending = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) {
      return;
    }
    pending += value;
    const lines = pending.split('\n');
    pending = lines.pop();
    for (const line of lines) {
      if (line.trim() !== '') {
        yield JSON.parse(line);
      }
    }
  }
}

// What a failed attempt means: the device is not held, the page cannot
// recover, or the attempt is worth repeating. A 401 and any code the
// server chose, other than the retryable ones, cannot recover, because
// the same request would be refused again. SESSION_CLOSED is one of
// them. A failure with no code at all is a refused connection, a proxy
// page or a broken stream, and is repeated up to the failure limit.
function verdict(err) {
  if (!(err instanceof ApiError)) {
    return 'retry';
  }
  if (DEVICE_UNAVAILABLE_CODES.has(err.code)) {
    return 'unavailable';
  }
  if (err.status === 401 || (err.coded && !RETRYABLE_CODES.has(err.code))) {
    return 'fatal';
  }
  return 'retry';
}

// Follows the capture feed. A line of kind tree or screenshot calls the
// matching handler. A stream that ends for any reason is reopened after
// a growing pause and the handlers run once on every reopen, so a
// capture missed in between is picked up. A feed that answers
// DEVICE_RELEASED or DEVICE_LOST stops for good until start is called
// again. A feed that cannot recover stops with onFatal at once, with the
// server's own message and remedy, and one that fails
// MAX_CONSECUTIVE_FAILURES attempts in a row stops with onFatal too. An
// attempt that opens the feed starts the count over.
export function createEventStream({ onTree, onScreenshot, onReleased, onFatal }) {
  let abort = null;

  async function run(signal) {
    let backoff = BACKOFF_START_MS;
    let failures = 0;
    let lastError = null;
    while (!signal.aborted) {
      try {
        const res = await request('/api/v1/events', { signal });
        backoff = BACKOFF_START_MS;
        failures = 0;
        onTree();
        onScreenshot();
        for await (const event of ndjson(res.body, signal)) {
          if (event.kind === 'tree') {
            onTree();
          } else if (event.kind === 'screenshot') {
            onScreenshot();
          }
        }
      } catch (err) {
        if (signal.aborted) {
          return;
        }
        const kind = verdict(err);
        if (kind === 'unavailable') {
          onReleased(err.code);
          return;
        }
        if (kind === 'fatal') {
          onFatal(err);
          return;
        }
        failures += 1;
        lastError = err;
      }
      if (failures >= MAX_CONSECUTIVE_FAILURES) {
        onFatal(lastError instanceof ApiError && lastError.coded ? lastError : new ApiError(0, GONE_ERROR));
        return;
      }
      await delay(backoff, signal);
      backoff = Math.min(backoff * 2, BACKOFF_MAX_MS);
    }
  }

  return {
    start() {
      this.stop();
      abort = new AbortController();
      run(abort.signal);
    },
    stop() {
      if (abort) {
        abort.abort();
        abort = null;
      }
    },
  };
}
