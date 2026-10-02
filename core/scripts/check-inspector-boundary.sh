#!/usr/bin/env bash
# Forbids internal/inspector from reaching the device any way other
# than session.Viewer and session.Controller.
#
# The inspector serves a browser, so everything it can call is reachable
# by whoever holds its token. It must therefore never take the built
# driver or the Orchestra from a session, because either one runs
# device operations outside the activity gate that keeps a fresh capture
# from interleaving with a tool call. The checked patterns are the
# .Built( and .Orchestra( accessors, the orchestra.Orchestra type, and
# any import of the driver package or its subpackages. Every file under
# the directory is checked, tests included, and comment-only lines are
# ignored so documentation may name the forbidden things.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

DIR="internal/inspector"
PATTERN='\.Built\(|\.Orchestra\(|orchestra\.Orchestra([^A-Za-z0-9_]|$)|"dev\.atomyx/internal/driver(/[^"]*)?"'

if [ ! -d "$DIR" ]; then
  echo "PASS: $DIR does not exist yet"
  exit 0
fi

violations=0

while IFS= read -r hit; do
  path="${hit%%:*}"
  rest="${hit#*:}"
  line="${rest%%:*}"
  content="${rest#*:}"
  trimmed="${content#"${content%%[![:space:]]*}"}"
  case "$trimmed" in
    //*) continue ;;
  esac
  echo "FORBIDDEN: $path:$line: the inspector may reach the device only through session.Viewer and session.Controller"
  violations=1
done < <(grep -rEn --include='*.go' "$PATTERN" "$DIR" || true)

if [ "$violations" -ne 0 ]; then
  echo ""
  echo "Do not call .Built(, .Orchestra( or use orchestra.Orchestra or the driver package under core/$DIR."
  exit 1
fi

echo "PASS: $DIR reaches the device only through the session ports"
exit 0
