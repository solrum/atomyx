import { getJson } from './api.js';

// Holder states in which the holder owns the device, so a fresh capture
// would interleave with what it is doing.
const BUSY_STATES = new Set(['running', 'borrowed']);

let blocked = false;

function setText(id, text) {
  document.getElementById(id).textContent = text || '-';
}

function holderText(info) {
  const command = info.holder && info.holder.command;
  return command ? `${command} (${info.state})` : info.state;
}

// Whether the fresh-capture control is unavailable because the holder is
// busy. The control reads it after its own request ends, so it never
// re-enables itself while the holder still runs.
export function refreshBlocked() {
  return blocked;
}

function showRefreshAvailability(info) {
  blocked = BUSY_STATES.has(info.state);
  const button = document.getElementById('refresh');
  const note = document.getElementById('refresh-note');
  button.disabled = blocked;
  note.textContent = blocked ? `The holder is ${info.state}, so a fresh capture is not available.` : '';
}

export async function refreshSession() {
  const info = await getJson('/api/v1/session');
  setText('session-device', info.device.name ? `${info.device.name} (${info.device.id})` : info.device.id);
  setText('session-platform', info.device.platform);
  setText('session-kind', info.device.kind);
  setText('session-holder', holderText(info));
  showRefreshAvailability(info);
  return info;
}
