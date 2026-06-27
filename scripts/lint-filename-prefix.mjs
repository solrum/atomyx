#!/usr/bin/env node
/**
 * Verifies that every file inside a features/<name>/ folder is either
 * named `index.ts(x)` or starts with `<name>.` / `<name>-`.
 *
 * Exits 0 when clean, 1 when violations are found.
 * Pass --json to receive machine-readable output.
 */

import { readdirSync, statSync } from "node:fs";
import { join, relative, basename, extname } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = fileURLToPath(new URL("..", import.meta.url));

const SKIP_DIRS = new Set(["node_modules", "dist", "build", ".git", "coverage", ".npm-cache"]);

const FEATURE_ROOTS = [
  join(ROOT, "apps", "studio", "src", "domain", "features"),
  join(ROOT, "apps", "studio", "src", "state", "features"),
  join(ROOT, "apps", "studio", "src", "platform", "features"),
  join(ROOT, "apps", "studio", "src", "ui", "features"),
];

// Also scan packages/*/src/features/ when it exists
function collectPackageFeatureRoots() {
  const pkgDir = join(ROOT, "packages");
  const roots = [];
  try {
    for (const pkg of readdirSync(pkgDir)) {
      const featuresDir = join(pkgDir, pkg, "src", "features");
      try {
        statSync(featuresDir);
        roots.push(featuresDir);
      } catch {
        // package has no features/ dir
      }
    }
  } catch {
    // packages/ doesn't exist
  }
  return roots;
}

function isHidden(name) {
  return name.startsWith(".");
}

function isSkipped(name) {
  return SKIP_DIRS.has(name) || isHidden(name);
}

/**
 * Recursively scan a feature folder.
 * All files at any depth must be prefixed with the feature name.
 * Subfolders do NOT reset the required prefix — prefix stays the feature name.
 */
function scanFeatureDir(featureName, dir, violations) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }

  for (const entry of entries) {
    if (isSkipped(entry)) continue;

    const fullPath = join(dir, entry);
    let stat;
    try {
      stat = statSync(fullPath);
    } catch {
      continue;
    }

    if (stat.isDirectory()) {
      scanFeatureDir(featureName, fullPath, violations);
      continue;
    }

    // Files: allow index.ts / index.tsx
    const base = basename(entry);
    if (base === "index.ts" || base === "index.tsx") continue;

    // Allow if starts with "<featureName>." or "<featureName>-"
    if (base.startsWith(featureName + ".") || base.startsWith(featureName + "-")) continue;

    violations.push({
      file: relative(ROOT, fullPath),
      feature: featureName,
      expected_prefix: featureName,
    });
  }
}

function collectAllFeatureRoots() {
  return [...FEATURE_ROOTS, ...collectPackageFeatureRoots()];
}

export function run() {
  const jsonMode = process.argv.includes("--json");
  const violations = [];

  for (const featuresRoot of collectAllFeatureRoots()) {
    let featureDirs;
    try {
      featureDirs = readdirSync(featuresRoot);
    } catch {
      continue;
    }

    for (const featureName of featureDirs) {
      if (isSkipped(featureName)) continue;

      const featureDir = join(featuresRoot, featureName);
      let stat;
      try {
        stat = statSync(featureDir);
      } catch {
        continue;
      }
      if (!stat.isDirectory()) continue;

      scanFeatureDir(featureName, featureDir, violations);
    }
  }

  if (jsonMode) {
    process.stdout.write(JSON.stringify(violations, null, 2) + "\n");
  } else {
    for (const v of violations) {
      process.stdout.write(
        `${v.file}:1: missing prefix '${v.expected_prefix}.' or '${v.expected_prefix}-'\n`
      );
    }
    if (violations.length > 0) {
      process.stdout.write(`\n${violations.length} filename-prefix violation(s) found.\n`);
    }
  }

  process.exitCode = violations.length > 0 ? 1 : 0;
  return violations;
}

run();
