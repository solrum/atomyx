const DEBOUNCE_MS = 150;

export function bindSearch(input, view) {
  let timer = null;
  input.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(() => view.setQuery(input.value), DEBOUNCE_MS);
  });
  input.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      clearTimeout(timer);
      if (input.value !== view.query()) {
        view.setQuery(input.value);
      }
      view.stepMatch(event.shiftKey ? -1 : 1);
      event.preventDefault();
    }
  });
}
