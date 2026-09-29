import atomyxScriptSchema from "./generated/atomyx-script.schema.json";

/**
 * JSON Schema for `*.atomyx.yml` scripts, staged from the Go core
 * (`go/internal/script`) by `npm run generate:schema`. Feeds
 * monaco-yaml for completion, hover, and validation. The Go core is
 * the single source of truth for this schema; Studio only consumes
 * the emitted artifact.
 */
export const scriptJsonSchema = atomyxScriptSchema;
