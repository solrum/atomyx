#!/usr/bin/env bash
# Confirms the committed JSON Schema files still match the Go
# structs they are generated from. Studio copies the script and config
# schemas at build time to validate scripts and config in its Monaco
# editor, so a struct changed without regenerating one of them would
# ship a stale contract to Studio silently. The run schema is read by
# external consumers of run folders.
#
# Generation writes to a temporary path first, then that output is
# compared byte for byte against the committed file. Regenerating
# straight over the committed path before comparing would erase any
# drift before it could be detected, since the freshly generated
# content is deterministic and would then trivially match itself.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

SCRIPT_SCHEMA="internal/script/atomyx-script.schema.json"
CONFIG_SCHEMA="internal/config/atomyx-config.schema.json"
RUN_SCHEMA="internal/runfolder/atomyx-run.schema.json"
INDEX_SCHEMA="internal/runfolder/atomyx-run-index.schema.json"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

SCRIPT_SCHEMA_FRESH="$TMP_DIR/atomyx-script.schema.json"
CONFIG_SCHEMA_FRESH="$TMP_DIR/atomyx-config.schema.json"
RUN_SCHEMA_FRESH="$TMP_DIR/atomyx-run.schema.json"
INDEX_SCHEMA_FRESH="$TMP_DIR/atomyx-run-index.schema.json"

go run ./cmd/gen-script-schema -out "$SCRIPT_SCHEMA_FRESH"
go run ./cmd/gen-config-schema -out "$CONFIG_SCHEMA_FRESH"
go run ./cmd/gen-run-schema -out "$RUN_SCHEMA_FRESH"
go run ./cmd/gen-index-schema -out "$INDEX_SCHEMA_FRESH"

stale=0

# check_fresh COMMITTED_PATH FRESH_PATH
# Reports and copies the fresh content over the committed path when
# they differ, so a developer running this locally is left with the
# regenerated file already in place: `git status` shows it as
# modified, ready to inspect and commit. A clean checkout in CI never
# takes this branch, so it never leaves CI's working tree dirty.
check_fresh() {
  local committed="$1"
  local fresh="$2"
  if ! cmp -s "$committed" "$fresh"; then
    echo "STALE: $committed no longer matches the Go structs it is generated from"
    cp "$fresh" "$committed"
    stale=1
  fi
}

check_fresh "$SCRIPT_SCHEMA" "$SCRIPT_SCHEMA_FRESH"
check_fresh "$CONFIG_SCHEMA" "$CONFIG_SCHEMA_FRESH"
check_fresh "$RUN_SCHEMA" "$RUN_SCHEMA_FRESH"
check_fresh "$INDEX_SCHEMA" "$INDEX_SCHEMA_FRESH"

if [ "$stale" -ne 0 ]; then
  echo ""
  echo "The committed JSON Schema is stale. Run 'make -C core gen-schema' and commit the result."
  exit 1
fi

echo "PASS: committed JSON Schema files match the Go structs"
exit 0
