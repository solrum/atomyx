#!/usr/bin/env node
/**
 * Scans source files for banned phrases defined in the comment and docs rules.
 * Exits 0 when clean, 1 when any violation is found.
 *
 * Inline suppression: a line ending with `// atomyx-allow-phrase` or
 * `// eslint-disable-next-line atomyx/banned-phrases` suppresses the
 * NEXT line.
 */

import { readdirSync, statSync, readFileSync } from "node:fs";
import { join, relative, extname } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = fileURLToPath(new URL("..", import.meta.url));

const SKIP_DIRS = new Set([
  "node_modules", "dist", "build", ".git", "coverage",
  ".npm-cache", "api-snapshots", "wire-snapshots", ".changeset",
  "scripts",
]);

const SCAN_EXTENSIONS = new Set([".ts", ".tsx", ".mjs", ".js", ".md", ".kt", ".swift"]);

/**
 * Each rule has:
 *   pattern  — RegExp applied to the line text
 *   message  — human-readable violation description
 *   skipFile — optional function(filePath) => boolean to skip certain files
 */
const RULES = [
  {
    pattern: /\b(Phase|Batch|Sprint|Week|Milestone)\s+\d+\b/,
    message: "milestone reference — use present-state description instead",
  },
  {
    pattern: /\b(legacy|deprecated|retired|removed)\s+(the\s+|this\s+)?\w+/i,
    message: "refers to a removed thing — describe the present, not the past",
    skipFile: (f) => f.includes("CHANGELOG") || f.includes("decisions/"),
  },
  {
    pattern: /\bSHIPPED\s*\(\d{4}/,
    message: "shipping marker — remove status markers from source files",
  },
  {
    pattern: /\bper\s+ADR-\d+\b/i,
    message: "ADR reference in code — state the rule inline instead",
  },
  {
    pattern: /\bsee\s+ADR-\d+\b/i,
    message: "ADR reference in code — state the rule inline instead",
  },
  {
    pattern: /\bobservation-driven\s+(refactor|wait\s+primitives)\b/i,
    message: "internal codename — use the current API name",
  },
  {
    pattern: /\bsidecar\s+(split|extraction)\b/i,
    message: "internal codename — use a present-state description",
  },
  {
    pattern: /\bfeature-api\s+migration\b/i,
    message: "internal codename — use a present-state description",
  },
  {
    pattern: /\bDI\s+migration\b/i,
    message: "internal codename — use a present-state description",
  },
  {
    pattern: /\bstate\s+feature\s+batch\b/i,
    message: "internal codename — use a present-state description",
  },
];

const INLINE_IGNORE_RE = /\/\/\s*(atomyx-allow-phrase|eslint-disable-next-line\s+atomyx\/banned-phrases)\s*$/;

/**
 * Detect comment-inside-template-literal lines.
 * Walks the source tracking backtick nesting depth.
 * When inside a template literal, flags lines that contain `// ` or `/* `.
 */
function findCommentInTemplateLiteral(lines, filePath) {
  const hits = [];
  let inTemplate = false;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    // Toggle inTemplate on unescaped backticks (simple heuristic — not
    // full JS parser; good enough for catching the obvious leak pattern)
    let j = 0;
    while (j < line.length) {
      if (line[j] === "\\" ) { j += 2; continue; }
      if (line[j] === "`") { inTemplate = !inTemplate; }
      j++;
    }

    if (inTemplate) {
      if (/\/\/ |\/\*/.test(line)) {
        hits.push({
          file: relative(ROOT, filePath),
          line: i + 1,
          col: line.indexOf("//") !== -1 ? line.indexOf("//") + 1 : line.indexOf("/*") + 1,
          message: "code-style comment inside template literal — may leak into user-facing string",
          match: (line.match(/\/\/[^\n]*|\/\*[^*]*\*\//)?.[0] ?? "").trim().slice(0, 60),
        });
      }
    }
  }
  return hits;
}

function isSkippedDir(name) {
  return SKIP_DIRS.has(name) || name.startsWith(".");
}

function walkFiles(dir, cb) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    if (isSkippedDir(entry)) continue;
    const fullPath = join(dir, entry);
    let stat;
    try { stat = statSync(fullPath); } catch { continue; }
    if (stat.isDirectory()) {
      walkFiles(fullPath, cb);
    } else if (SCAN_EXTENSIONS.has(extname(entry))) {
      cb(fullPath);
    }
  }
}

export function run() {
  const violations = [];

  walkFiles(ROOT, (filePath) => {
    const rel = relative(ROOT, filePath);
    let content;
    try {
      content = readFileSync(filePath, "utf8");
    } catch {
      return;
    }

    const lines = content.split("\n");
    const isTs = extname(filePath) === ".ts" || extname(filePath) === ".tsx";

    let suppressNext = false;

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      const lineNum = i + 1;

      if (suppressNext) {
        suppressNext = false;
        continue;
      }

      if (INLINE_IGNORE_RE.test(line)) {
        suppressNext = true;
        continue;
      }

      for (const rule of RULES) {
        if (rule.skipFile && rule.skipFile(rel)) continue;
        const m = rule.pattern.exec(line);
        if (!m) continue;
        const col = m.index + 1;
        violations.push({
          file: rel,
          line: lineNum,
          col,
          message: rule.message,
          match: m[0].slice(0, 60),
        });
      }
    }

    // Extra check for TS/TSX: comment-inside-template-literal
    if (isTs) {
      const extra = findCommentInTemplateLiteral(lines, filePath);
      violations.push(...extra);
    }
  });

  // Sort by file then line
  violations.sort((a, b) => {
    const fc = a.file.localeCompare(b.file);
    return fc !== 0 ? fc : a.line - b.line;
  });

  for (const v of violations) {
    process.stdout.write(
      `${v.file}:${v.line}:${v.col}: ${v.message} — matched: "${v.match}"\n`
    );
  }

  if (violations.length > 0) {
    process.stdout.write(`\n${violations.length} banned-phrase violation(s) found.\n`);
  }

  process.exitCode = violations.length > 0 ? 1 : 0;
  return violations;
}

run();
