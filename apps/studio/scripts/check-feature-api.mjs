#!/usr/bin/env node
/**
 * Structural gate: every top-level export function in a feature's
 * index.ts must be named with a `create` prefix (factory) or `use`
 * prefix (React hook). Any other name is the deleted zero-arg
 * singleton-accessor antipattern.
 *
 * The same rule applies to `export const` arrow functions that
 * perform a registry lookup — they must also begin with `create`
 * or `use`.
 *
 * Run: node scripts/check-feature-api.mjs
 * Exit 0 on success, 1 on violation.
 */

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = fileURLToPath(new URL(".", import.meta.url));
const featuresDir = resolve(__dirname, "../src/state/features");

const ALLOWED_PREFIXES = ["create", "use"];

// Patterns that are explicitly allowed even though they don't start
// with create/use: KEY constants, type-only helpers, and utility
// functions that are not feature accessors.
// We match only `export function` and `export const ... = () =>` forms.
const EXPORT_FUNCTION_RE = /^export\s+function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/m;
const EXPORT_CONST_FN_RE =
  /^export\s+const\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=\s*(?:\([^)]*\)\s*(?::\s*[^=]+)?\s*=>|function\s*\()/m;

// Non-function exports that are fine: type aliases, value constants.
// We only care about callable (function) exports.
const EXPORT_FN_GLOBAL_RE =
  /^export\s+function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/gm;
const EXPORT_CONST_FN_GLOBAL_RE =
  /^export\s+const\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=\s*(?:\([^)]*\)\s*(?::\s*[^=\n]+)?\s*=>|function\s*\()/gm;

function isAllowed(name) {
  return ALLOWED_PREFIXES.some((p) => name.startsWith(p));
}

// Some names are structural helpers exported from index but not
// factories or hooks — they are legitimate. We recognize them by
// convention: if they are not zero-arg and the name does not look
// like a getter/accessor, they pass. For this check we treat any
// export function name that doesn't start with create/use as a
// violation UNLESS it is in the explicit allowlist below (helpers
// that predated this rule and are not accessors).
const ALLOWLIST = new Set([
  // ui-inspector helpers — pure functions, not registry accessors
  "resolveUiNode",
  "computeTreeExtent",
  // actions helper — not a registry accessor (takes arguments)
  "registerActionHandler",
  // workspace-state setup function — not a registry accessor
  "installWorkspaceStatePersistence",
]);

let violations = 0;
let featuresChecked = 0;

for (const entry of readdirSync(featuresDir)) {
  const featureDir = join(featuresDir, entry);
  if (!statSync(featureDir).isDirectory()) continue;

  const indexPath = join(featureDir, "index.ts");
  let source;
  try {
    source = readFileSync(indexPath, "utf8");
  } catch {
    // No index.ts — skip (should not happen per feature-structure rules)
    continue;
  }

  featuresChecked++;

  // Check all export function declarations
  for (const match of source.matchAll(EXPORT_FN_GLOBAL_RE)) {
    const name = match[1];
    if (!isAllowed(name) && !ALLOWLIST.has(name)) {
      const lineNumber =
        source.slice(0, match.index).split("\n").length;
      console.error(
        `${indexPath}:${lineNumber}: export function '${name}' does not start with 'create' or 'use' — looks like a deleted singleton accessor. Use getFeature<T>(KEY) or useXxx() instead.`,
      );
      violations++;
    }
  }

  // Check all export const arrow/function expressions
  for (const match of source.matchAll(EXPORT_CONST_FN_GLOBAL_RE)) {
    const name = match[1];
    if (!isAllowed(name) && !ALLOWLIST.has(name)) {
      const lineNumber =
        source.slice(0, match.index).split("\n").length;
      console.error(
        `${indexPath}:${lineNumber}: export const '${name}' is a callable that does not start with 'create' or 'use' — looks like a deleted singleton accessor alias. Use getFeature<T>(KEY) or useXxx() instead.`,
      );
      violations++;
    }
  }
}

if (violations === 0) {
  console.log(`OK: ${featuresChecked} features checked`);
  process.exit(0);
} else {
  console.error(`\n${violations} violation(s) found across ${featuresChecked} features.`);
  process.exit(1);
}
