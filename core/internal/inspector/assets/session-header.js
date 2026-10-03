import { getJson } from './api.js';

function setText(id, text) {
  document.getElementById(id).textContent = text || '-';
}

export async function refreshSession() {
  const info = await getJson('/api/v1/session');
  setText('session-device', info.device.name ? `${info.device.name} (${info.device.id})` : info.device.id);
  setText('session-platform', info.device.platform);
  setText('session-kind', info.device.kind);
  setText('session-holder', info.state);
  return info;
}
