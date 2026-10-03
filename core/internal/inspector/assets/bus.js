const listeners = new Map();

// A tiny publish and subscribe hub. Topics in use:
//   tree       payload is the /api/v1/tree response, announced after the
//              tree view has taken it in.
//   selection  payload is { path, revision, node } for the selected row.
//              path is the child index chain from the root, the
//              shape the selector routes take, and revision is the
//              tree revision the path was read from.
export function on(topic, listener) {
  if (!listeners.has(topic)) {
    listeners.set(topic, new Set());
  }
  listeners.get(topic).add(listener);
  return () => listeners.get(topic).delete(listener);
}

export function emit(topic, payload) {
  for (const listener of listeners.get(topic) || []) {
    listener(payload);
  }
}
