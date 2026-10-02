#!/usr/bin/env bash
# Forbids a bare syscall.Open( or unix.Open( call anywhere in the core
# module outside internal/lease. A file opened through the raw
# syscall does not get the close-on-exec flag that os.OpenFile sets,
# so its descriptor leaks into every child process the module spawns
# (adb, xcodebuild, iproxy). A leaked lease descriptor is the worst
# case: a child that outlives its parent keeps the device flock held
# after the owning process has exited. Every file is therefore opened
# with os.OpenFile, which applies close-on-exec the same way for every
# file the process opens.
#
# internal/lease is the only allowlisted directory, because it is the
# one package that owns raw descriptors and its tests prove the
# close-on-exec behavior directly. Comment-only lines are ignored, so
# documentation may name the forbidden call.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

ALLOWED_DIR="internal/lease/"
PATTERN='(^|[^A-Za-z0-9_])(syscall|unix)\.Open\('

violations=0

while IFS= read -r hit; do
  path="${hit%%:*}"
  rest="${hit#*:}"
  line="${rest%%:*}"
  content="${rest#*:}"
  case "$path" in
    ./"$ALLOWED_DIR"*) continue ;;
  esac
  trimmed="${content#"${content%%[![:space:]]*}"}"
  case "$trimmed" in
    //*) continue ;;
  esac
  echo "FORBIDDEN: ${path#./}:$line: raw open call, use os.OpenFile so close-on-exec applies"
  violations=1
done < <(grep -rEn --include='*.go' "$PATTERN" . || true)

if [ "$violations" -ne 0 ]; then
  echo ""
  echo "A bare syscall.Open( or unix.Open( is only allowed under core/$ALLOWED_DIR."
  exit 1
fi

echo "PASS: no raw syscall.Open or unix.Open outside core/$ALLOWED_DIR"
exit 0
