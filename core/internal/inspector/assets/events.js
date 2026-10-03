import { ApiError, DEVICE_UNAVAILABLE_CODES, request } from './api.js';

const BACKOFF_START_MS = 500;
const BACKOFF_MAX_MS = 8000;

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

// Follows the capture feed. A line of kind tree or screenshot calls the
// matching handler. A stream that ends for any reason is reopened after
// a growing pause and the handlers run once on every reopen, so a
// capture missed in between is picked up. A feed that answers
// DEVICE_RELEASED or DEVICE_LOST stops for good until start is called
// again.
export function createEventStream({ onTree, onScreenshot, onReleased, onFatal }) {
  let abort = null;

  async function run(signal) {
    let backoff = BACKOFF_START_MS;
    while (!signal.aborted) {
      try {
        const res = await request('/api/v1/events', { signal });
        backoff = BACKOFF_START_MS;
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
        if (err instanceof ApiError && DEVICE_UNAVAILABLE_CODES.has(err.code)) {
          onReleased(err.code);
          return;
        }
        if (err instanceof ApiError && err.status === 401) {
          onFatal(err);
          return;
        }
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
