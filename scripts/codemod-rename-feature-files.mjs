#!/usr/bin/env node
/**
 * Renames files under features/<name>/ that lack the required <name>. or <name>- prefix,
 * then updates all import/require statements across apps/studio/src/ that reference the old names.
 *
 * Usage:
 *   node scripts/codemod-rename-feature-files.mjs [--dry-run]
 *
 * Safety:
 *   - Aborts if a rename target already exists (collision).
 *   - Idempotent: running twice is a no-op (violations list will be empty on second run).
 *   - Uses git mv when available; falls back to fs.rename + git add.
 */

import { readdirSync, statSync, readFileSync, writeFileSync, renameSync, existsSync } from "node:fs";
import { join, relative, basename, dirname, extname } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const ROOT = fileURLToPath(new URL("..", import.meta.url));
const STUDIO_SRC = join(ROOT, "apps", "studio", "src");
const DRY_RUN = process.argv.includes("--dry-run");

// ---------------------------------------------------------------------------
// Step 1: collect violations using the same logic as lint-filename-prefix.mjs
// ---------------------------------------------------------------------------

const SKIP_DIRS = new Set(["node_modules", "dist", "build", ".git", "coverage", ".npm-cache"]);

const FEATURE_ROOTS = [
  join(ROOT, "apps", "studio", "src", "domain", "features"),
  join(ROOT, "apps", "studio", "src", "state", "features"),
  join(ROOT, "apps", "studio", "src", "platform", "features"),
  join(ROOT, "apps", "studio", "src", "ui", "features"),
];

function isSkipped(name) {
  return SKIP_DIRS.has(name) || name.startsWith(".");
}

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
    const base = basename(entry);
    if (base === "index.ts" || base === "index.tsx") continue;
    if (base.startsWith(featureName + ".") || base.startsWith(featureName + "-")) continue;
    violations.push({ file: relative(ROOT, fullPath), feature: featureName, fullPath });
  }
}

function collectViolations() {
  const violations = [];
  for (const featuresRoot of FEATURE_ROOTS) {
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
  return violations;
}

// ---------------------------------------------------------------------------
// Step 2: compute target filename for each violation
// ---------------------------------------------------------------------------

// These compound suffixes (checked first) always get a dot separator.
const DOT_COMPOUND_SUFFIXES = [
  ".port.ts", ".port.tsx",
  ".mock.ts", ".mock.tsx",
  ".contract.ts", ".contract.tsx",
  ".test.ts", ".test.tsx",
  ".spec.ts", ".spec.tsx",
  ".zustand.ts",
  ".impl.ts",
  ".tauri.ts", ".tauri.tsx",
  ".fs.ts",
  ".node.ts",
  ".dispatcher.ts", ".dispatcher.tsx",
];

// Simple suffixes that also use a dot separator (for plain files like "types.ts")
const DOT_SIMPLE_SUFFIXES = new Set([
  "types.ts", "types.tsx",
  "tokens.ts",
  "store.ts", "store.tsx",
  "service.ts", "service.tsx",
  "handlers.ts", "handlers.tsx",
]);

function isTypeClassKind(filename) {
  for (const suffix of DOT_COMPOUND_SUFFIXES) {
    if (filename.endsWith(suffix)) return true;
  }
  return DOT_SIMPLE_SUFFIXES.has(filename);
}

function computeTarget(feature, oldBasename) {
  if (oldBasename.startsWith(feature + ".") || oldBasename.startsWith(feature + "-")) {
    return oldBasename; // already valid
  }
  const sep = isTypeClassKind(oldBasename) ? "." : "-";
  return feature + sep + oldBasename;
}

// ---------------------------------------------------------------------------
// Step 3: collect all source files for import rewriting
// ---------------------------------------------------------------------------

function walkSrcFiles(dir, result = []) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return result;
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
      walkSrcFiles(fullPath, result);
    } else {
      const ext = extname(entry).toLowerCase();
      if ([".ts", ".tsx", ".css", ".json"].includes(ext)) {
        result.push(fullPath);
      }
    }
  }
  return result;
}

// ---------------------------------------------------------------------------
// Step 4: rewrite imports in a file
// ---------------------------------------------------------------------------

/**
 * Build a normalized relative path from srcDir → targetDir/targetFile, producing
 * the path string as it would appear inside an import (i.e., starting with "./" or "../").
 */
function buildImportPath(srcDir, targetAbsPath, includeStem = true) {
  const targetDir = dirname(targetAbsPath);
  const targetBase = basename(targetAbsPath);
  let rel = relative(srcDir, targetDir).replace(/\\/g, "/");
  // Ensure relative paths within the same directory use "./" prefix
  if (rel === "" || rel === ".") {
    rel = ".";
  }
  if (!rel.startsWith("..") && !rel.startsWith(".")) {
    rel = "./" + rel;
  }
  if (rel === ".") {
    rel = "./";
  }
  if (includeStem) {
    return rel.endsWith("/") ? rel + targetBase : rel + "/" + targetBase;
  }
  return rel;
}

/**
 * Escape a string for use in a RegExp.
 */
function escRe(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Rewrite import statements in a source file.
 * Returns { changed: boolean, newContent: string, editsCount: number }.
 */
function rewriteImports(filePath, renameMap) {
  let content;
  try {
    content = readFileSync(filePath, "utf8");
  } catch {
    return { changed: false, newContent: "", editsCount: 0 };
  }

  let changed = false;
  let editsCount = 0;
  const srcDir = dirname(filePath);

  for (const [oldAbsPath, newAbsPath] of renameMap.entries()) {
    const oldBase = basename(oldAbsPath);
    const newBase = basename(newAbsPath);

    const hasBareExtension = /\.(css|json)$/.test(oldBase);

    if (hasBareExtension) {
      // CSS / JSON: import uses full filename WITH extension
      // e.g. import "./tokens.css" or import "../features/theme/app.css"
      const oldImportPath = buildImportPath(srcDir, oldAbsPath, true);
      const newImportPath = buildImportPath(srcDir, newAbsPath, true);
      const re = new RegExp(`(["'])${escRe(oldImportPath)}(["'])`, "g");
      const next = content.replace(re, (_m, q1, q2) => {
        editsCount++;
        changed = true;
        return `${q1}${newImportPath}${q2}`;
      });
      content = next;
    } else {
      // TS/TSX: import uses stem (no .ts/.tsx), but may use .js extension in path
      const oldStem = oldBase.replace(/\.(ts|tsx)$/, "");
      const newStem = newBase.replace(/\.(ts|tsx)$/, "");

      // Compute the import path to the OLD file's stem (as TypeScript sees it)
      const oldImportBase = buildImportPath(srcDir, oldAbsPath, false);
      // Now append the stem to that directory path
      const dirPart = oldImportBase.endsWith("/") ? oldImportBase : oldImportBase + "/";
      const oldPathNoExt = dirPart + oldStem;
      const oldPathJs = dirPart + oldStem + ".js";

      const newDirPart = (() => {
        const d = buildImportPath(srcDir, newAbsPath, false);
        return d.endsWith("/") ? d : d + "/";
      })();
      const newPathNoExt = newDirPart + newStem;
      const newPathJs = newDirPart + newStem + ".js";

      // Try matching both with and without .js extension
      for (const [oldPath, newPath] of [
        [oldPathNoExt, newPathNoExt],
        [oldPathJs, newPathJs],
      ]) {
        const re = new RegExp(`(["'])${escRe(oldPath)}(["'])`, "g");
        const next = content.replace(re, (_m, q1, q2) => {
          editsCount++;
          changed = true;
          return `${q1}${newPath}${q2}`;
        });
        content = next;
      }
    }
  }

  return { changed, newContent: content, editsCount };
}

// ---------------------------------------------------------------------------
// Step 5: git mv or fallback rename
// ---------------------------------------------------------------------------

function gitMv(oldPath, newPath) {
  const result = spawnSync("git", ["mv", oldPath, newPath], {
    cwd: ROOT,
    encoding: "utf8",
  });
  return result.status === 0;
}

function fsRename(oldPath, newPath) {
  renameSync(oldPath, newPath);
  spawnSync("git", ["add", newPath], { cwd: ROOT });
  spawnSync("git", ["rm", "--cached", oldPath], { cwd: ROOT });
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

function main() {
  const violations = collectViolations();

  if (violations.length === 0) {
    console.log("No violations found — nothing to rename.");
    return;
  }

  // Build rename plan
  const plan = [];
  const collisions = [];

  for (const v of violations) {
    const oldBase = basename(v.fullPath);
    const newBase = computeTarget(v.feature, oldBase);

    if (newBase === oldBase) continue; // already valid

    const oldFullPath = v.fullPath;
    const newFullPath = join(dirname(oldFullPath), newBase);

    if (existsSync(newFullPath)) {
      collisions.push({ old: v.file, new: relative(ROOT, newFullPath) });
      continue;
    }

    plan.push({ oldFullPath, newFullPath, oldBase, newBase, feature: v.feature });
  }

  if (collisions.length > 0) {
    console.error("ABORT: the following rename targets already exist (collision):");
    for (const c of collisions) {
      console.error(`  ${c.old} → ${c.new}`);
    }
    process.exit(1);
  }

  // Build rename map for import rewriting
  const renameMap = new Map();
  for (const p of plan) {
    renameMap.set(p.oldFullPath, p.newFullPath);
  }

  // Collect all source files across apps/studio/src/
  const sourceFiles = walkSrcFiles(STUDIO_SRC);

  // Compute import edits (before renaming, so old paths exist)
  const importEdits = [];
  for (const srcFile of sourceFiles) {
    const { changed, newContent, editsCount } = rewriteImports(srcFile, renameMap);
    if (changed) {
      importEdits.push({ srcFile, newContent, editsCount });
    }
  }

  // Report plan
  console.log(`\n=== Rename plan: ${plan.length} files ===`);
  for (const p of plan) {
    console.log(`  [${p.feature}] ${p.oldBase} → ${p.newBase}`);
  }

  const totalImportEdits = importEdits.reduce((acc, e) => acc + e.editsCount, 0);
  const importFileList = importEdits.map((e) => `  ${relative(ROOT, e.srcFile)} (${e.editsCount} edits)`).join("\n");
  console.log(`\n=== Import edits: ${totalImportEdits} occurrences in ${importEdits.length} files ===`);
  console.log(importFileList);

  if (DRY_RUN) {
    console.log("\n[DRY RUN] No changes applied.");
    return;
  }

  // Apply import edits FIRST (while old file paths still exist, paths were computed from them)
  console.log("\n=== Applying import edits... ===");
  for (const e of importEdits) {
    writeFileSync(e.srcFile, e.newContent, "utf8");
    console.log(`  Updated: ${relative(ROOT, e.srcFile)} (${e.editsCount} edits)`);
  }

  // Apply renames
  console.log("\n=== Renaming files... ===");
  let renamedCount = 0;
  for (const p of plan) {
    const ok = gitMv(p.oldFullPath, p.newFullPath);
    if (!ok) {
      fsRename(p.oldFullPath, p.newFullPath);
    }
    console.log(`  ${ok ? "git mv" : "fs mv"}: ${p.oldBase} → ${p.newBase}`);
    renamedCount++;
  }

  console.log(
    `\n=== Done: ${renamedCount} files renamed, ${totalImportEdits} import edits applied ===`
  );
}

main();
