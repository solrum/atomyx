import * as monaco from "monaco-editor";
import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import yamlWorker from "monaco-yaml/yaml.worker?worker";
import { configureMonacoYaml, type JSONSchema } from "monaco-yaml";
import { scriptJsonSchema } from "../../../domain/features/scripts/index.js";
import { installErrorOnlyMarkerFilter } from "./editor-marker-filter.js";
import { installProblemsSubscriber } from "./editor-problems-subscriber.js";

const SCRIPT_SCHEMA_URI = "atomyx://schema/script";

let ready = false;

/**
 * Configure the Monaco environment exactly once per window.
 * - Binds `yaml` / default language labels to the right workers.
 * - Registers the Atomyx script JSON Schema from the domain layer.
 *
 * Safe to call from multiple component mounts; only the first call
 * does anything.
 */
export function ensureMonacoReady(): void {
  if (ready) return;
  ready = true;

  (self as unknown as { MonacoEnvironment: monaco.Environment }).MonacoEnvironment = {
    getWorker(_moduleId, label) {
      if (label === "yaml") return new yamlWorker();
      return new editorWorker();
    },
  };

  configureMonacoYaml(monaco, {
    enableSchemaRequest: false,
    hover: true,
    completion: true,
    validate: true,
    format: true,
    schemas: [
      {
        uri: SCRIPT_SCHEMA_URI,
        fileMatch: [
          "*.atomyx.yml",
          "*.atomyx.yaml",
          "atomyx-script.yml",
          "inmemory://atomyx-script.yml",
          "inmemory://atomyx-script-*.yml",
        ],
        schema: scriptJsonSchema as unknown as JSONSchema,
      },
    ],
  });

  installErrorOnlyMarkerFilter(monaco);
  installProblemsSubscriber(monaco);
}

export { monaco };
