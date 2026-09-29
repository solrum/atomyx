#!/usr/bin/env node
/**
 * Copies the JSON Schema files emitted by the core runtime into a
 * Studio-local generated directory Monaco can import directly.
 *
 * The core runtime (core/internal/script, core/internal/config) is the
 * single source of truth for the script and config schemas. This
 * script does not derive or transform anything; it only stages the two
 * emitted files where the TypeScript build can see them, since the Go
 * source tree sits outside the Studio project's TypeScript project
 * references.
 */
import { copyFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");
const outDir = resolve(
  here,
  "..",
  "src",
  "domain",
  "features",
  "scripts",
  "generated",
);

const SOURCES = [
  {
    from: resolve(repoRoot, "core", "internal", "script", "atomyx-script.schema.json"),
    to: resolve(outDir, "atomyx-script.schema.json"),
  },
  {
    from: resolve(repoRoot, "core", "internal", "config", "atomyx-config.schema.json"),
    to: resolve(outDir, "atomyx-config.schema.json"),
  },
];

mkdirSync(outDir, { recursive: true });

for (const { from, to } of SOURCES) {
  copyFileSync(from, to);
  process.stdout.write(`Copied   ${from} -> ${to}\n`);
}
