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

export function showReleased(text) {
  show('released', text);
}

export function hideReleased() {
  document.getElementById('released').hidden = true;
}
