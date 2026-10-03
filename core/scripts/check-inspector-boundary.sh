#!/usr/bin/env bash
# Forbids internal/inspector from reaching the device, or the run
# folder, any way other than the session ports it is handed: the Viewer,
# the Refresher, the StepSource and its own lifecycle port.
#
# The inspector serves a browser, so everything it can call is reachable
# by whoever holds its token. It must therefore never take the built
# driver or the Orchestra from a session, because either one runs
# device operations outside the activity gate that keeps a fresh capture
# from interleaving with a tool call. It must also never acquire or
# attach a device itself, because the composition in internal/cli
# decides who holds the device and hands the inspector an opener. It
# must never write run folder lines, because only the holder of the
# device records a run. The checked patterns are the .Built( and
# .Orchestra( accessors, the orchestra.Orchestra type, any import of the
# driver package or its subpackages, session.Open( and session.Attach(
# calls, and the run folder writers AppendStep, AppendIntervention and
# runfolder.Writer. Every file under the directory is checked, tests
# included, and comment-only lines are ignored so documentation may name
# the forbidden things.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

DIR="internal/inspector"
PATTERN='\.Built\(|\.Orchestra\(|orchestra\.Orchestra([^A-Za-z0-9_]|$)|"dev\.atomyx/internal/driver(/[^"]*)?"|session\.Open\(|session\.Attach\(|AppendStep|AppendIntervention|runfolder\.Writer'

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
  echo "FORBIDDEN: $path:$line: the inspector may reach the device and the run folder only through its session ports"
  violations=1
done < <(grep -rEn --include='*.go' "$PATTERN" "$DIR" || true)

if [ "$violations" -ne 0 ]; then
  echo ""
  echo "Do not call .Built(, .Orchestra(, session.Open( or session.Attach(, write run folder lines, or use orchestra.Orchestra or the driver package under core/$DIR."
  echo "Take the device through the opener the composition in internal/cli hands over."
  exit 1
fi

echo "PASS: $DIR reaches the device only through the session ports"
exit 0
