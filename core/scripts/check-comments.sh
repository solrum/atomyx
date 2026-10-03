#!/usr/bin/env bash
# Forbids two classes of problem in Go source: any mention of
# TypeScript in a production file (a comment that anchors current
# behavior to the TypeScript implementation, such as "the TypeScript
# CLI" or "mirrors the TypeScript port", or any other use of the
# word), and
# prose that uses the em dash or the right arrow as a stand-in for a
# connecting word. Once the TypeScript implementation retires, a
# comment pointing at it becomes a ghost reference: it names something
# a future reader cannot open and cannot verify. A comment should
# describe what the Go code does, not what it was derived from. A
# symbol used in place of a word (an em dash for "because" or "so", an
# arrow for "then" or "maps to") should instead be the word itself, so
# a reader never has to infer the missing connective from punctuation.
#
# Scope for the TypeScript-reference check: every case-insensitive
# occurrence of the word "typescript" in production *.go files
# only. Test files (*_test.go) and generated files (path contains
# zz_generated) are exempt, because a test comparing Go output against
# a reference fixture legitimately names that fixture.
#
# Scope for the banned-character check: every *.go file, including
# tests, because a test file's comments and log lines are prose too.
#
# Existing hits are grandfathered by file: scripts/comment-gate-allowlist.txt
# for the TypeScript-reference check (matched by substring), and
# scripts/comment-gate-dash-allowlist.txt for the banned-character
# check (matched by exact path:line). Both files may only shrink:
# every entry represents a comment that still needs rewording, or a
# string kept byte-identical to a TypeScript oracle for parity, never
# new debt. Do not add an entry for a comment written after this gate
# existed; fix the comment instead.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

violations=0

# ---------------------------------------------------------------------------
# Check 1: no comment anchors current behavior to the TypeScript implementation.
# ---------------------------------------------------------------------------

FORBIDDEN_PATTERN='typescript'
TS_ALLOWLIST_FILE="scripts/comment-gate-allowlist.txt"

# is_ts_allowlisted PATH LINE_CONTENT
# An allowlist entry is "path:substring". A hit is grandfathered when its
# file matches the entry's path and its line contains the entry's
# substring. Matching by substring rather than by line number survives
# unrelated edits that shift line numbers in the same file.
is_ts_allowlisted() {
  local hit_path="$1"
  local hit_content="$2"
  local entry entry_path entry_substring
  while IFS= read -r entry; do
    [ -z "$entry" ] && continue
    case "$entry" in
      \#*) continue ;;
    esac
    entry_path="${entry%%:*}"
    entry_substring="${entry#*:}"
    if [ "$entry_path" = "$hit_path" ] && [[ "$hit_content" == *"$entry_substring"* ]]; then
      return 0
    fi
  done < "$TS_ALLOWLIST_FILE"
  return 1
}

# Parsed with parameter expansion rather than `read file line content`: a
# comment line ending in ":" (a common way to introduce a list) would lose
# that trailing colon to `read`'s multi-variable field splitting, which
# silently drops one trailing IFS delimiter from the last field.
while IFS= read -r raw_hit; do
  file="${raw_hit%%:*}"
  rest="${raw_hit#*:}"
  line="${rest%%:*}"
  content="${rest#*:}"
  if [ -f "$TS_ALLOWLIST_FILE" ] && is_ts_allowlisted "$file" "$content"; then
    continue
  fi
  echo "${file}:${line}: forbidden TypeScript reference; describe what the Go code does instead"
  violations=$((violations + 1))
done < <(grep -rniE "$FORBIDDEN_PATTERN" --include="*.go" . \
  | grep -v '_test\.go:' \
  | grep -v 'zz_generated' \
  | sed 's/^\.\///')

# ---------------------------------------------------------------------------
# Check 2: no Go file uses the em dash or the right arrow as an idea
# separator or word substitute. Write the connecting word instead
# (because, so, then, maps to, and so on), or split into two sentences.
# ---------------------------------------------------------------------------

DASH_ALLOWLIST_FILE="scripts/comment-gate-dash-allowlist.txt"

# is_dash_allowlisted PATH LINE
# A dash-allowlist entry is the exact "path:line". Unlike the
# TypeScript-reference allowlist, this one matches by line number, not
# substring, because a grandfathered entry here names a specific
# string kept byte-identical to a TypeScript oracle for parity: the
# exact wording must not drift, so matching on wording would defeat
# the point.
is_dash_allowlisted() {
  local hit_path="$1"
  local hit_line="$2"
  local entry
  while IFS= read -r entry; do
    [ -z "$entry" ] && continue
    case "$entry" in
      \#*) continue ;;
    esac
    if [ "$entry" = "${hit_path}:${hit_line}" ]; then
      return 0
    fi
  done < "$DASH_ALLOWLIST_FILE"
  return 1
}

# Built via printf's octal escapes, rather than embedding the
# characters literally, so this file's own bytes never trip the rule
# it enforces. \342\200\224 is U+2014 (em dash) and \342\206\222 is
# U+2192 (right arrow) in UTF-8; grep -E takes them as literal bytes
# since none of them is a regex metacharacter, so this works with any
# POSIX grep and does not depend on PCRE (grep -P) support.
BANNED_CHAR_PATTERN=$(printf '\342\200\224|\342\206\222')

while IFS= read -r raw_hit; do
  file="${raw_hit%%:*}"
  rest="${raw_hit#*:}"
  line="${rest%%:*}"
  if [ -f "$DASH_ALLOWLIST_FILE" ] && is_dash_allowlisted "$file" "$line"; then
    continue
  fi
  echo "${file}:${line}: forbidden em dash or arrow; write the connecting word instead"
  violations=$((violations + 1))
done < <(grep -rnE "$BANNED_CHAR_PATTERN" --include="*.go" . | sed 's/^\.\///')

if [ "$violations" -gt 0 ]; then
  echo ""
  echo "FAIL: $violations violation(s) found outside the allowlists"
  exit 1
fi

echo "PASS: no forbidden TypeScript self-references, em dashes, or arrows found"
exit 0
