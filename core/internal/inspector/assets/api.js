const TOKEN_HEADER = 'X-Atomyx-Token';

let token = null;

export function setToken(value) {
  token = value;
}

export class ApiError extends Error {
  constructor(status, info) {
    super(info.message || `request failed with status ${status}`);
    this.status = status;
    this.code = info.code || 'UNKNOWN';
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
