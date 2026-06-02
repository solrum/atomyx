#!/usr/bin/env node
// Bundle the sidecar + every workspace dep into a single CJS file
// that a host (Tauri Rust) can spawn as `node sidecar.cjs`.
//
// Keeps production packaging contained here — Tauri just copies
// the produced artifact into its resource bundle, no node_modules
// tree-walking at install time.

import { build } from "esbuild";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { mkdirSync } from "node:fs";

const here = dirname(fileURLToPath(import.meta.url));
const outdir = resolve(here, "dist-bundle");
mkdirSync(outdir, { recursive: true });

await build({
  entryPoints: [resolve(here, "src/cli.ts")],
  outfile: resolve(outdir, "sidecar.cjs"),
  bundle: true,
  platform: "node",
  target: "node20",
  format: "cjs",
  sourcemap: true,
  logLevel: "info",
  // node: built-ins resolve natively at runtime on the host; don't
  // bundle them. Everything else (workspace packages, npm deps)
  // gets inlined so the single artifact has no import chain to
  // follow.
  external: [],
});

console.log(`Bundled → ${resolve(outdir, "sidecar.cjs")}`);
