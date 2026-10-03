export function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) {
    node.className = className;
  }
  if (text !== undefined) {
    node.textContent = text;
  }
  return node;
}

// Adds a titled section to the details pane and returns its body, which
// the panel fills with textContent and element children only, never
// markup built from device text.
export function addPanel(host, title) {
  const section = el('section', 'panel');
  section.append(el('h2', 'panel-title', title));
  const body = el('div', 'panel-body');
  section.append(body);
  host.append(section);
  return body;
}

export function describeError(err) {
  return err.remedy ? `${err.message} ${err.remedy}` : err.message;
}

// A one line summary of a selector for a list, for example
// `id: login, role: button`. A regular expression pattern shows as its
// JSON form.
export function summarizeSelector(selector) {
  return Object.entries(selector)
    .map(([key, value]) => `${key}: ${typeof value === 'object' ? JSON.stringify(value) : value}`)
    .join(', ');
}
