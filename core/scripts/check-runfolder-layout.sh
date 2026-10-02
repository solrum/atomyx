#!/usr/bin/env bash
# Forbids the run folder layout names from appearing as string literals
# in production Go code outside internal/runfolder. The names are runs,
# v1, artifacts, report, and screenshots. They are the on-disk contract
# of a run folder, so one package owns them and every other package
# asks it for a path. A second spelling would let a rename or a layout
# change miss a caller silently.
#
# A line is flagged when it is not a comment line, is not in a _test.go
# file, and matches one of these shapes:
#
#   1. A call argument that is exactly runs, v1, artifacts, or
#      screenshots, such as filepath.Join(dir, "screenshots").
#   2. The same four names as the whole right-hand side of an
#      assignment, such as screenshotSubDir = "screenshots".
#   3. A path-join call (Join or join) with a quoted report argument.
#      The bare word report is common in config keys and error text,
#      so it is only flagged inside a join.
#   4. A quoted path that contains a slash and has runs, artifacts,
#      screenshots, or report as a whole segment, such as "runs/v1" or
#      "report/x.html". A bare v1 segment is not flagged here, because
#      format identifiers such as atomyx/v1 end in it.
#   5. A link format that embeds screenshots/%s or artifacts/%s.
#
# Struct tags (json:"artifacts"), map keys ("report": ...), and index
# expressions (root["report"]) are never flagged, because they name wire
# or config fields, not directories.
#
# ALLOWLIST holds the production files that still spell a layout name.
# Each entry is a file that a later step moves onto internal/runfolder,
# and the entry is deleted in that step. Do not add an entry for new
# code.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

OWN_DIR="internal/runfolder/"

# Remove each entry when the step named beside it lands.
ALLOWLIST=(
  # The report renderer links screenshots as screenshots/<file> and
  # the reporter writes into a screenshots directory. Moves in the CLI
  # wiring step.
  "internal/report/render_html.go"
  "internal/report/fs_reporter.go"
  # The MCP screenshot tool names its own screenshots directory and
  # the run read tools list the flat runs key. Move in the MCP read
  # path and MCP wiring steps.
  "internal/mcp/screenshot_tool.go"
  "internal/mcp/run_read_tool.go"
  # The flat run record key prefix. Moves in the read path step.
  "internal/storage/records.go"
)

NAMES='runs|v1|artifacts|screenshots'
ARG="[(,][[:space:]]*\"($NAMES)\"[[:space:]]*[,)]"
ASSIGN="=[[:space:]]*\"($NAMES)\"[[:space:]]*\$"
JOIN="[Jj]oin\\(.*\"report\""
SLASHED="\"([^\"[:space:]]*/)?(runs|artifacts|screenshots|report)(/[^\"[:space:]]*)?\""
LINK='(screenshots|artifacts)/%s'
PATTERN="$ARG|$ASSIGN|$JOIN|$LINK"

is_allowed() {
  local path="$1" entry
  for entry in "${ALLOWLIST[@]}"; do
    [ "$path" = "$entry" ] && return 0
  done
  return 1
}

violations=0

report_hit() {
  local hit="$1" path rest line content trimmed
  path="${hit%%:*}"
  path="${path#./}"
  rest="${hit#*:}"
  line="${rest%%:*}"
  content="${rest#*:}"
  case "$path" in
    "$OWN_DIR"* | *_test.go) return ;;
  esac
  is_allowed "$path" && return
  trimmed="${content#"${content%%[![:space:]]*}"}"
  case "$trimmed" in
    //*) return ;;
  esac
  echo "FORBIDDEN: $path:$line: run folder layout name spelled outside internal/runfolder"
  violations=1
}

while IFS= read -r hit; do
  report_hit "$hit"
done < <(grep -rEn --include='*.go' "$PATTERN" . || true)

# Shape 4 needs a slash inside the literal, so a plain word never matches.
while IFS= read -r hit; do
  content="${hit#*:*:}"
  trimmed="${content#"${content%%[![:space:]]*}"}"
  # An import line is a lone quoted path, optionally aliased.
  if [[ "$trimmed" =~ ^([A-Za-z_.]+[[:space:]]+)?\"[^\"]*\"$ ]]; then
    continue
  fi
  case "$content" in
    *'"'*/*'"'*) report_hit "$hit" ;;
  esac
done < <(grep -rEn --include='*.go' "$SLASHED" . || true)

if [ "$violations" -ne 0 ]; then
  echo ""
  echo "Use the path helpers in core/internal/runfolder instead of spelling runs, v1,"
  echo "artifacts, report, or screenshots in another package."
  exit 1
fi

echo "PASS: run folder layout names are spelled only in core/$OWN_DIR"
exit 0
