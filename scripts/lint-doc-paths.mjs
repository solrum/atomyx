#!/usr/bin/env node
/**
 * Scans all .md files and verifies that file paths referenced in them
 * actually exist on disk.
 *
 * Exits 0 when clean, 1 when any ghost reference is found.
 *
 * Inline suppression: a line `<!-- atomyx-allow-path: <path> -->` suppresses
 * one occurrence of that path in the next paragraph (consumed on first use).
 */

import { readdirSync, statSync, readFileSync, existsSync } from "node:fs";
import { join, dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = fileURLToPath(new URL("..", import.meta.url));

const SKIP_DIRS = new Set([
  "node_modules", ".git", "dist", "build", "coverage",
  ".npm-cache", ".changeset",
]);

// Extensions that qualify a bare word as a file path reference
const PATH_EXTENSIONS = new Set([
  ".ts", ".tsx", ".md", ".mjs", ".js", ".cjs",
  ".kt", ".swift", ".json", ".yaml", ".yml", ".toml", ".lock",
]);

// Path-like prefixes: if a candidate starts with one of these it is treated
// as a file path regardless of extension
const PATH_PREFIXES = [
  "./", "../", "packages/", "apps/", "platforms/", "shared/",
  "scripts/", ".claude/", "src/",
];

function isUrl(s) {
  return /^https?:\/\/|^mailto:/i.test(s);
}

function hasVariables(s) {
  return /[${}\\<>]/.test(s);
}

function hasGlob(s) {
  return /[*?[\]]/.test(s);
}

function looksLikePath(s) {
  if (isUrl(s) || hasVariables(s) || hasGlob(s)) return false;
  if (!s.includes("/") && !PATH_EXTENSIONS.has("." + s.split(".").pop())) return false;
  for (const p of PATH_PREFIXES) {
    if (s.startsWith(p)) return true;
  }
  const ext = "." + s.split(".").pop();
  return PATH_EXTENSIONS.has(ext) && s.includes("/");
}

const ALLOW_PATH_RE = /<!--\s*atomyx-allow-path:\s*(.+?)\s*-->/g;

// Regexes to extract candidates from markdown text
const INLINE_CODE_RE = /`([^`\n]+)`/g;
const MARKDOWN_LINK_RE = /\[[^\]]*\]\(([^)]+)\)/g;

function extractCandidates(content) {
  const results = []; // [{path, line}]
  const lines = content.split("\n");

  // Build a line-number index by scanning cumulatively
  let offset = 0;
  const lineStarts = lines.map((l) => {
    const s = offset;
    offset += l.length + 1;
    return s;
  });

  function lineOfOffset(charOffset) {
    let lo = 0, hi = lineStarts.length - 1;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (lineStarts[mid] <= charOffset) lo = mid;
      else hi = mid - 1;
    }
    return lo + 1; // 1-based
  }

  // Inline code
  let m;
  INLINE_CODE_RE.lastIndex = 0;
  while ((m = INLINE_CODE_RE.exec(content)) !== null) {
    const candidate = m[1].trim();
    if (looksLikePath(candidate)) {
      results.push({ path: candidate, line: lineOfOffset(m.index) });
    }
  }

  // Markdown links  [text](path)
  MARKDOWN_LINK_RE.lastIndex = 0;
  while ((m = MARKDOWN_LINK_RE.exec(content)) !== null) {
    const candidate = m[1].split(" ")[0].trim(); // strip optional title
    if (looksLikePath(candidate)) {
      results.push({ path: candidate, line: lineOfOffset(m.index) });
    }
  }

  return results;
}

function isSkippedDir(name) {
  return SKIP_DIRS.has(name) || name.startsWith(".");
}

function walkMdFiles(dir, cb) {
  let entries;
  try { entries = readdirSync(dir); } catch { return; }
  for (const entry of entries) {
    if (isSkippedDir(entry)) continue;
    const fullPath = join(dir, entry);
    let stat;
    try { stat = statSync(fullPath); } catch { continue; }
    if (stat.isDirectory()) {
      walkMdFiles(fullPath, cb);
    } else if (entry.endsWith(".md")) {
      cb(fullPath);
    }
  }
}

export function run() {
  const violations = [];

  walkMdFiles(ROOT, (mdPath) => {
    let content;
    try { content = readFileSync(mdPath, "utf8"); } catch { return; }

    const rel = relative(ROOT, mdPath);
    const mdDir = dirname(mdPath);

    // Collect allowed paths from suppression comments (consumed once on use)
    const allowedPaths = new Map(); // path -> count remaining
    let am;
    ALLOW_PATH_RE.lastIndex = 0;
    while ((am = ALLOW_PATH_RE.exec(content)) !== null) {
      const p = am[1].trim();
      allowedPaths.set(p, (allowedPaths.get(p) ?? 0) + 1);
    }

    const candidates = extractCandidates(content);

    for (const { path: candidate, line } of candidates) {
      // Try to resolve relative to the md file's directory first,
      // then relative to ROOT as a fallback (for repo-root-relative paths)
      const resolvedFromMd = resolve(mdDir, candidate);
      const resolvedFromRoot = resolve(ROOT, candidate);

      if (existsSync(resolvedFromMd) || existsSync(resolvedFromRoot)) continue;

      // Check suppression
      if (allowedPaths.has(candidate) && allowedPaths.get(candidate) > 0) {
        allowedPaths.set(candidate, allowedPaths.get(candidate) - 1);
        continue;
      }

      violations.push({
        file: rel,
        line,
        path: candidate,
      });
    }
  });

  // Sort by file then line
  violations.sort((a, b) => {
    const fc = a.file.localeCompare(b.file);
    return fc !== 0 ? fc : a.line - b.line;
  });

  for (const v of violations) {
    process.stdout.write(`${v.file}:${v.line}: path does not exist — "${v.path}"\n`);
  }

  if (violations.length > 0) {
    process.stdout.write(`\n${violations.length} ghost path reference(s) found.\n`);
  }

  process.exitCode = violations.length > 0 ? 1 : 0;
  return violations;
}

run();
