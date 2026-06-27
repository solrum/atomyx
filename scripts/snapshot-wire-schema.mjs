#!/usr/bin/env node
/**
 * Wire schema snapshot tool.
 *
 * Converts every public zod schema exported from @atomyx/shared
 * into JSON Schema and writes the results to wire-snapshots/.
 *
 * Usage:
 *   node scripts/snapshot-wire-schema.mjs           # generate + write
 *   node scripts/snapshot-wire-schema.mjs --check   # verify against committed snapshots
 *
 * The --check mode exits 1 when any snapshot does not match the
 * currently generated schema, reporting the diff on stdout.
 * Use it in CI to detect accidental wire-contract changes.
 */

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, "..");
const SNAPSHOT_DIR = join(ROOT, "wire-snapshots");

const CHECK = process.argv.includes("--check");

// ── Load shared dist (built output) ───────────────────────────────
const require = createRequire(import.meta.url);
// Use the built dist so the snapshot reflects the published surface.
const sharedDist = join(ROOT, "shared", "dist", "script");
const zodToJsonSchema = require("zod-to-json-schema").default ?? require("zod-to-json-schema");

async function loadSchemas() {
  const scriptSchema = await import(`file://${join(sharedDist, "script-schema.js")}`);
  const scenarioSchema = await import(`file://${join(sharedDist, "scenario-schema.js")}`);

  return [
    { name: "ScriptDefinition", schema: scriptSchema.ScriptDefinitionSchema },
    { name: "ScriptSelector",   schema: scriptSchema.ScriptSelectorSchema },
    { name: "PointerTarget",    schema: scriptSchema.PointerTargetSchema },
    { name: "PointerAction",    schema: scriptSchema.PointerActionSchema },
    { name: "PointerGroup",     schema: scriptSchema.PointerGroupSchema },
    { name: "ScenarioDefinition", schema: scenarioSchema.ScenarioDefinitionSchema },
  ];
}

function schemaToJson(name, schema) {
  const json = zodToJsonSchema(schema, {
    name,
    $refStrategy: "none",
    errorMessages: false,
  });
  return JSON.stringify(json, null, 2) + "\n";
}

async function main() {
  const schemas = await loadSchemas();

  mkdirSync(SNAPSHOT_DIR, { recursive: true });

  let failed = false;

  for (const { name, schema } of schemas) {
    const generated = schemaToJson(name, schema);
    const snapshotFile = join(SNAPSHOT_DIR, `${name}.json`);

    if (CHECK) {
      let committed;
      try {
        committed = readFileSync(snapshotFile, "utf8");
      } catch {
        process.stdout.write(
          `MISSING  ${name}.json — run without --check to generate\n`,
        );
        failed = true;
        continue;
      }
      if (generated !== committed) {
        process.stdout.write(`MISMATCH ${name}.json\n`);
        // Simple diff: show first differing line
        const a = committed.split("\n");
        const b = generated.split("\n");
        for (let i = 0; i < Math.max(a.length, b.length); i++) {
          if (a[i] !== b[i]) {
            process.stdout.write(
              `  line ${i + 1}: committed=${JSON.stringify(a[i])} generated=${JSON.stringify(b[i])}\n`,
            );
            break;
          }
        }
        failed = true;
      } else {
        process.stdout.write(`OK       ${name}.json\n`);
      }
    } else {
      writeFileSync(snapshotFile, generated, "utf8");
      process.stdout.write(`Wrote    ${name}.json\n`);
    }
  }

  if (CHECK && failed) {
    process.stderr.write(
      "\nWire schema snapshots are stale. Regenerate with:\n" +
        "  node scripts/snapshot-wire-schema.mjs\n",
    );
    process.exit(1);
  }
}

main().catch((err) => {
  process.stderr.write(`${err.message}\n`);
  process.exit(1);
});
