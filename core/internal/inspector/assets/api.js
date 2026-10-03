const TOKEN_HEADER = 'X-Atomyx-Token';

let token = null;

export function setToken(value) {
  token = value;
}

// Codes of a device the inspector does not hold: given up after sitting
// idle, lost, or a holder that exited. The operator brings it back with the reconnect control.
export const DEVICE_UNAVAILABLE_CODES = new Set(['DEVICE_RELEASED', 'DEVICE_LOST', 'HOLDER_EXITED']);

export class ApiError extends Error {
  constructor(status, info) {
    super(info.message || `request failed with status ${status}`);
    this.status = status;
    this.code = info.code || 'UNKNOWN';
    // True when the body carried a code, so the server itself answered
    // and the failure is not a proxy page or a broken connection.
    this.coded = Boolean(info.code);
    this.remedy = info.remedy || '';
  }
}

async function failure(res) {
  let info = {};
  try {
    info = (await res.json()).error || {};
  } catch {
    info = {};
  }
  return new ApiError(res.status, info);
}

export async function request(path, { method = 'GET', body, signal } = {}) {
  const headers = { [TOKEN_HEADER]: token };
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  const res = await fetch(path, {
    method,
    headers,
    signal,
    cache: 'no-store',
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    throw await failure(res);
  }
  return res;
}

export async function getJson(path) {
  return (await request(path)).json();
}

export async function postJson(path, body = {}) {
  return (await request(path, { method: 'POST', body })).json();
}

export async function exchangeCode(code) {
  const res = await fetch('/api/v1/auth', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code }),
  });
  if (!res.ok) {
    throw await failure(res);
  }
  setToken((await res.json()).token);
}

export function coalesced(task) {
  let running = false;
  let again = false;
  return async () => {
    if (running) {
      again = true;
      return;
    }
    running = true;
    try {
      do {
        again = false;
        await task();
      } while (again);
    } finally {
      running = false;
    }
  };
}
