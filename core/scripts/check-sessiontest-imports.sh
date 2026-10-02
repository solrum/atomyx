#!/usr/bin/env bash
# Forbids a non-test Go file from importing internal/session/sessiontest.
# That package holds helpers that stand in for production wiring, such as
# a lease manager over a throwaway directory. A production file that
# imports it would ship the stand-in, so only _test.go files, and the
# package's own files, may import it.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

IMPORT='"dev.atomyx/internal/session/sessiontest"'
OWN_DIR="internal/session/sessiontest/"

violations=0

while IFS= read -r path; do
  case "$path" in
    ./"$OWN_DIR"* | *_test.go) continue ;;
  esac
  echo "FORBIDDEN: ${path#./}: non-test file imports internal/session/sessiontest"
  violations=1
done < <(grep -rlF --include='*.go' "$IMPORT" . || true)

if [ "$violations" -ne 0 ]; then
  echo ""
  echo "internal/session/sessiontest may only be imported from _test.go files."
  exit 1
fi

echo "PASS: internal/session/sessiontest is imported only by tests"
exit 0
