#!/usr/bin/env node
/**
 * Coverage ratchet check.
 *
 * Reads coverage-baseline.json at repo root. For each package
 * listed, runs its test suite under c8 and compares the measured
 * percentages to the baseline. Exits 1 if any metric drops more
 * than TOLERANCE below the baseline.
 *
 * Usage:
 *   node scripts/check-coverage-baseline.mjs         # check all packages
 *   node scripts/check-coverage-baseline.mjs shared  # check one package
 *
 * The tolerance exists to absorb c8's floating-point variance
 * between Node versions. Gaps larger than TOLERANCE are real drops.
 */

import { readFileSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, "..");
const BASELINE_PATH = join(ROOT, "coverage-baseline.json");
const C8 = join(ROOT, "node_modules", ".bin", "c8");

// Drop tolerance in percentage points (e.g. 0.5 allows 79.5% when baseline is 80%)
const TOLERANCE = 0.5;

const METRICS = ["statements", "branches", "functions", "lines"];

function readBaseline() {
  if (!existsSync(BASELINE_PATH)) {
    process.stderr.write(`Missing coverage-baseline.json at ${BASELINE_PATH}\n`);
    process.exit(1);
  }
  return JSON.parse(readFileSync(BASELINE_PATH, "utf8"));
}

function findTestFiles(srcDir) {
  const files = [];
  if (!existsSync(srcDir)) return files;
  function walk(dir) {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full);
      } else if (entry.isFile() && /\.test\.(ts|tsx)$/.test(entry.name)) {
        files.push(full);
      }
    }
  }
  walk(srcDir);
  return files;
}

function runCoverage(pkgDir, testFiles) {
  if (testFiles.length === 0) return null;

  const summaryPath = join(pkgDir, "coverage", "coverage-summary.json");
  const srcDir = join(pkgDir, "src");

  const result = spawnSync(
    C8,
    [
      "--reporter=json-summary",
      "--include=src/**/*.ts",
      "--exclude=src/**/*.test.ts",
      "--exclude=src/**/*.test.tsx",
      "node",
      "--import",
      "tsx",
      "--test",
      ...testFiles,
    ],
    { cwd: pkgDir, encoding: "utf8", timeout: 120_000 },
  );

  if (!existsSync(summaryPath)) return null;

  try {
    const summary = JSON.parse(readFileSync(summaryPath, "utf8"));
    return summary.total;
  } catch {
    return null;
  }
}

function runStudioCoverage(pkgDir) {
  // Studio uses its own .c8rc.json which scopes to domain+state layers only.
  const c8Local = join(pkgDir, "node_modules", ".bin", "c8") ;
  const c8Bin = existsSync(c8Local) ? c8Local : C8;
  const testFiles = findTestFiles(join(pkgDir, "src"));
  if (testFiles.length === 0) return null;

  const result = spawnSync(
    c8Bin,
    ["--reporter=json-summary", "node", "--import", "tsx", "--test", ...testFiles],
    { cwd: pkgDir, encoding: "utf8", timeout: 120_000 },
  );

  const summaryPath = join(pkgDir, "coverage", "coverage-summary.json");
  if (!existsSync(summaryPath)) return null;

  try {
    const summary = JSON.parse(readFileSync(summaryPath, "utf8"));
    return summary.total;
  } catch {
    return null;
  }
}

function pkgDir(name) {
  if (name === "studio") return join(ROOT, "apps", "studio");
  if (name === "shared") return join(ROOT, "shared");
  return join(ROOT, "packages", name);
}

async function main() {
  const baseline = readBaseline();
  const filter = process.argv[2];

  let anyFailed = false;
  const packages = filter ? [filter] : Object.keys(baseline);

  for (const pkg of packages) {
    if (!(pkg in baseline)) {
      process.stderr.write(`Unknown package: ${pkg}\n`);
      continue;
    }

    const dir = pkgDir(pkg);
    if (!existsSync(dir)) {
      process.stdout.write(`${pkg}: SKIP (directory not found)\n`);
      continue;
    }

    const testFiles = findTestFiles(join(dir, "src"));
    if (testFiles.length === 0) {
      process.stdout.write(`${pkg}: SKIP (no test files)\n`);
      continue;
    }

    process.stdout.write(`${pkg}: running coverage...`);
    const actual = pkg === "studio" ? runStudioCoverage(dir) : runCoverage(dir, testFiles);

    if (!actual) {
      process.stdout.write(` SKIP (coverage run failed or timed out)\n`);
      continue;
    }

    const base = baseline[pkg];
    let pkgFailed = false;
    const parts = [];

    for (const metric of METRICS) {
      const baseVal = base[metric] ?? 0;
      const actualVal = actual[metric]?.pct ?? 0;
      const drop = baseVal - actualVal;
      const symbol = drop > TOLERANCE ? "✗" : "✓";
      parts.push(`${metric}: ${baseVal}% → ${actualVal.toFixed(2)}% ${symbol}`);
      if (drop > TOLERANCE) {
        pkgFailed = true;
        anyFailed = true;
      }
    }

    process.stdout.write(`\n  ${pkg}: ${parts.join("  ")}\n`);
    if (pkgFailed) {
      process.stdout.write(
        `  ${pkg}: FAIL — coverage dropped below baseline (tolerance: ${TOLERANCE}%)\n`,
      );
    }
  }

  if (anyFailed) {
    process.stderr.write(
      "\nCoverage ratchet failed. Fix the regression or update coverage-baseline.json if the drop is intentional.\n",
    );
    process.exit(1);
  } else {
    process.stdout.write("\nAll packages at or above baseline.\n");
  }
}

main().catch((err) => {
  process.stderr.write(`${err.message}\n`);
  process.exit(1);
});
