import { ScriptDefinitionSchema } from "@atomyx/shared/script";
import { zodToJsonSchema } from "zod-to-json-schema";

/**
 * JSON Schema derived from the authoritative zod
 * `ScriptDefinitionSchema` in `@atomyx/shared`. Feeds
 * monaco-yaml for completion / hover / validation on
 * `*.atomyx.yml` files. Regenerated at module load — cheap
 * enough for a once-per-window call.
 */
export const scriptJsonSchema = zodToJsonSchema(ScriptDefinitionSchema, {
  name: "AtomyxScript",
  $refStrategy: "none",
});
