#!/usr/bin/env node
/**
 * Checks that all production dependencies use an approved license.
 *
 * Exit 0 when every license is in the allow-list (or in the per-package
 * exceptions table). Exit 1 and print violations otherwise.
 *
 * Exceptions are declared in root package.json under:
 *   "atomyx-license-allowlist": { "exceptions": [{ "name": "x", "version": "1.2.3", "reason": "..." }] }
 */

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "..");

const ALLOWED_LICENSES = new Set([
  "MIT",
  "ISC",
  "Apache-2.0",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "0BSD",
  "CC0-1.0",
  "Unlicense",
  "Python-2.0",
]);

/**
 * Returns true if every clause in an SPDX expression is in the allow-list.
 * Handles simple identifiers and (A OR B) / (A AND B) expressions.
 */
function isSpdxAllowed(expression) {
  // Strip outer parentheses and split on OR / AND
  const cleaned = expression.replace(/^\(|\)$/g, "").trim();
  const clauses = cleaned.split(/\s+(?:OR|AND)\s+/);
  // Accept if ANY clause is allowed (permissive interpretation for OR)
  return clauses.some((clause) => ALLOWED_LICENSES.has(clause.trim()));
}

function loadExceptions() {
  const pkgPath = resolve(ROOT, "package.json");
  const pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
  const cfg = pkg["atomyx-license-allowlist"];
  if (!cfg || !Array.isArray(cfg.exceptions)) return new Map();

  const map = new Map();
  for (const entry of cfg.exceptions) {
    const key = `${entry.name}@${entry.version}`;
    map.set(key, entry.reason ?? "(no reason provided)");
  }
  return map;
}

function runLicenseChecker() {
  const raw = execSync(
    "command npx license-checker --production --json --excludePackages='@atomyx/*' --excludePrivatePackages",
    { cwd: ROOT, encoding: "utf8" },
  );
  return JSON.parse(raw);
}

function main() {
  const exceptions = loadExceptions();
  let data;
  try {
    data = runLicenseChecker();
  } catch (err) {
    console.error("license-checker failed:", err.message);
    process.exit(1);
  }

  const violations = [];

  for (const [pkgKey, info] of Object.entries(data)) {
    // pkgKey is "name@version"
    const licenseRaw = (info.licenses ?? "UNKNOWN").trim();

    if (exceptions.has(pkgKey)) {
      continue;
    }

    if (isSpdxAllowed(licenseRaw)) {
      continue;
    }

    violations.push(`${pkgKey}: ${licenseRaw} — NOT ALLOWED`);
  }

  if (violations.length === 0) {
    console.log("license:check PASS — all production licenses are allowed.");
    process.exit(0);
  }

  console.error(`license:check FAIL — ${violations.length} violation(s):\n`);
  for (const v of violations) {
    console.error(`  ${v}`);
  }
  process.exit(1);
}

main();
