const NOTICE_MS = 6000;

let noticeTimer = null;

function show(id, text) {
  const el = document.getElementById(id);
  el.textContent = text;
  el.hidden = false;
}

export function showFatal(err) {
  const remedy = err.remedy ? ` ${err.remedy}` : '';
  show('fatal', `${err.message}${remedy}`);
}

export function showNotice(text) {
  show('notice', text);
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => {
    document.getElementById('notice').hidden = true;
  }, NOTICE_MS);
}

const UNAVAILABLE_TEXT = {
  DEVICE_RELEASED: 'The inspector released the device after it sat idle.',
  DEVICE_LOST: 'The device was lost. Check that it is connected and awake, then reconnect.',
};

export function unavailableText(code) {
  return UNAVAILABLE_TEXT[code] || UNAVAILABLE_TEXT.DEVICE_RELEASED;
}

export function showReleased(text) {
  document.getElementById('released-text').textContent = text;
  document.getElementById('released').hidden = false;
}

export function hideReleased() {
  document.getElementById('released').hidden = true;
}
