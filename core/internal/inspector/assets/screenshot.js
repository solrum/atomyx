import { emit } from './bus.js';
import { request } from './api.js';

let currentUrl = null;

function numberHeader(res, name) {
  return Number(res.headers.get(name));
}

// Fetches the newest screenshot and announces it on the bus together
// with what the server says about it: its revision, the scale from
// device units to screenshot pixels, and whether it is paired with the
// tree and can carry a highlight.
export async function refreshScreenshot() {
  const res = await request('/api/v1/screenshot');
  const facts = {
    revision: numberHeader(res, 'X-Atomyx-Revision'),
    scale: numberHeader(res, 'X-Atomyx-Scale'),
    paired: res.headers.get('X-Atomyx-Paired') === 'true',
    highlightable: res.headers.get('X-Atomyx-Highlightable') === 'true',
  };
  const url = URL.createObjectURL(await res.blob());
  const img = document.getElementById('screenshot');
  img.src = url;
  img.hidden = false;
  document.getElementById('screenshot-empty').hidden = true;
  if (currentUrl) {
    URL.revokeObjectURL(currentUrl);
  }
  currentUrl = url;
  try {
    await img.decode();
  } catch {
    return;
  }
  emit('screenshot', facts);
}
