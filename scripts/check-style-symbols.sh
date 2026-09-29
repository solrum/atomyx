#!/usr/bin/env bash
#
# Repo style gate: forbid the em dash (U+2014) and the right arrow
# (U+2192) as prose separators or word substitutes.
#
# A symbol standing in for a connecting word ("so", "then", "because",
# "instead") reads worse than the word and does not travel across
# languages. Prose spells the word; code, paths, and data keep their
# symbols.
#
# Ownership: Go source files (core/**/*.go) are covered by
# core/scripts/check-comments.sh, so this gate skips them to avoid a
# second owner. This gate owns the rest of the prose scope, that is the
# core/ non-source files (config plus the embedded skill and agent
# Markdown), the root scripts/ directory, the top-level Makefile, the
# native agent projects under platforms/, and every shipped Markdown
# doc.
#
# Out of scope (excluded): apps/studio/ (swept on its own track),
# core/internal/conformance/corpus/ (byte-parity fixtures and captures),
# core/internal/conformance/goldens/ (recorded outputs of those same
# fixtures, so they echo whatever symbols the fixtures contain),
# examples/ (test-script data), and any node_modules/ tree.
#
# BSD grep (macOS) has no -P, so the two code points are matched by
# their UTF-8 byte sequences under LC_ALL=C. This is portable to the GNU
# grep on CI as well. The em dash U+2014 is the octal byte sequence
# \342\200\224 and the arrow U+2192 is \342\206\222.
set -eu

cd "$(git rev-parse --show-toplevel)"

report="$(mktemp "${TMPDIR:-/tmp}/atomyx-style-gate.XXXXXX")"
trap 'rm -f "$report"' EXIT

git ls-files -z | while IFS= read -r -d '' f; do
  case "$f" in
    apps/studio/*|examples/*) continue ;;
    */node_modules/*|node_modules/*) continue ;;
    core/internal/conformance/corpus/*) continue ;;
    core/internal/conformance/goldens/*) continue ;;
    core/*.go) continue ;;
  esac
  case "$f" in
    Makefile|core/*|scripts/*|platforms/*|*.md) ;;
    *) continue ;;
  esac
  [ -f "$f" ] || continue
  LC_ALL=C grep -nHI -e $'\342\200\224' -e $'\342\206\222' -- "$f" >> "$report" || true
done

if [ -s "$report" ]; then
  echo "Style gate FAILED: em dash (U+2014) or right arrow (U+2192) found in prose scope." >&2
  echo "Rewrite each as the word it stands in for (so, then, because, instead, and, including)." >&2
  echo "A leading '- ' list marker is fine; the ban is the mid-sentence dash and the between-steps arrow." >&2
  echo "" >&2
  cat "$report" >&2
  exit 1
fi

echo "Style gate passed: no em dash or right arrow in the prose scope."
