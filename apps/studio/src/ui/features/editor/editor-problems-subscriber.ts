import type * as monacoNs from "monaco-editor";
import { getFeature } from "../../../state/core/registry.js";
import type { ProblemsApi } from "../../../state/features/problems/index.js";
import { PROBLEMS_KEY, type Problem } from "../../../state/features/problems/index.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";

let installed = false;

function severityFrom(
  monaco: typeof monacoNs,
  sev: monacoNs.MarkerSeverity,
): Problem["severity"] {
  if (sev === monaco.MarkerSeverity.Error) return "error";
  if (sev === monaco.MarkerSeverity.Warning) return "warning";
  if (sev === monaco.MarkerSeverity.Info) return "info";
  return "hint";
}

export function installProblemsSubscriber(monaco: typeof monacoNs): void {
  if (installed) return;
  installed = true;

  monaco.editor.onDidChangeMarkers(() => {
    const activePath = getFeature<EditorApi>(EDITOR_KEY).getSnapshot().activePath;
    if (!activePath) {
      getFeature<ProblemsApi>(PROBLEMS_KEY).set([]);
      return;
    }
    const models = monaco.editor.getModels();
    if (models.length === 0) {
      getFeature<ProblemsApi>(PROBLEMS_KEY).set([]);
      return;
    }
    const collected: Problem[] = [];
    for (const model of models) {
      const markers = monaco.editor.getModelMarkers({ resource: model.uri });
      for (const m of markers) {
        collected.push({
          path: activePath,
          line: m.startLineNumber,
          column: m.startColumn,
          severity: severityFrom(monaco, m.severity),
          message: m.message,
          source: m.source ?? null,
        });
      }
    }
    collected.sort((a, b) => {
      const sev = severityRank(a.severity) - severityRank(b.severity);
      if (sev !== 0) return sev;
      return a.line - b.line || a.column - b.column;
    });
    getFeature<ProblemsApi>(PROBLEMS_KEY).set(collected);
  });
}

function severityRank(s: Problem["severity"]): number {
  switch (s) {
    case "error":
      return 0;
    case "warning":
      return 1;
    case "info":
      return 2;
    case "hint":
      return 3;
  }
}
