export function showFatal(err) {
  const remedy = err.remedy ? ` ${err.remedy}` : '';
  const el = document.getElementById('fatal');
  el.textContent = `${err.message}${remedy}`;
  el.hidden = false;
}
