import type * as monacoNs from "monaco-editor";

let installed = false;
let filtering = false;

/**
 * Strip every non-error marker from the YAML models so the editor
 * only surfaces red squigglies — warnings, info hints, and schema
 * "might be nicer" suggestions are suppressed.
 *
 * Implementation: Monaco fires `onDidChangeMarkers` after any
 * owner (monaco-yaml, built-ins) writes markers. We read them back
 * via `getModelMarkers`, filter in-place, re-write under the same
 * owner. A reentrancy flag prevents the re-write from re-triggering
 * our own handler into a loop.
 *
 * Idempotent: safe to call on every editor mount — only the first
 * call installs the listener.
 */
export function installErrorOnlyMarkerFilter(
  monaco: typeof monacoNs,
): void {
  if (installed) return;
  installed = true;

  monaco.editor.onDidChangeMarkers((uris) => {
    if (filtering) return;
    for (const uri of uris) {
      const model = monaco.editor.getModel(uri);
      if (!model) continue;
      const markers = monaco.editor.getModelMarkers({ resource: uri });
      if (markers.length === 0) continue;

      const errorsOnly = markers.filter(
        (m) => m.severity === monaco.MarkerSeverity.Error,
      );
      if (errorsOnly.length === markers.length) continue;

      const owners = new Set(markers.map((m) => m.owner));
      filtering = true;
      try {
        for (const owner of owners) {
          const filteredForOwner = errorsOnly.filter(
            (m) => m.owner === owner,
          );
          monaco.editor.setModelMarkers(model, owner, filteredForOwner);
        }
      } finally {
        filtering = false;
      }
    }
  });
}
