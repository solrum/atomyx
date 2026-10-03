import { request } from './api.js';

let currentUrl = null;

export async function refreshScreenshot() {
  const res = await request('/api/v1/screenshot');
  const url = URL.createObjectURL(await res.blob());
  const img = document.getElementById('screenshot');
  img.src = url;
  img.hidden = false;
  document.getElementById('screenshot-empty').hidden = true;
  if (currentUrl) {
    URL.revokeObjectURL(currentUrl);
  }
  currentUrl = url;
}
