#!/usr/bin/env bash
# Forbids a non-test Go file from importing a test-support package.
#
# internal/session/sessiontest holds helpers that stand in for production
# wiring, such as a lease manager over a throwaway directory.
# internal/driver/drivertest holds the behavioral contract suite every
# driver must pass. A production file that imports either would ship test
# code, so only _test.go files, and the package's own files, may import
# them. The layer rules cannot enforce this, because they treat a test
# file like any other file of its package and so must allow the edges
# the tests need.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Each entry is the import path, then the directory whose files may
# import it without being a test.
PACKAGES=(
  "dev.atomyx/internal/session/sessiontest internal/session/sessiontest/"
  "dev.atomyx/internal/driver/drivertest internal/driver/drivertest/"
)

violations=0

for entry in "${PACKAGES[@]}"; do
  import_path="${entry%% *}"
  own_dir="${entry##* }"
  while IFS= read -r path; do
    case "$path" in
      ./"$own_dir"* | *_test.go) continue ;;
    esac
    echo "FORBIDDEN: ${path#./}: non-test file imports ${import_path#dev.atomyx/}"
    violations=1
  done < <(grep -rlF --include='*.go' "\"$import_path\"" . || true)
done

if [ "$violations" -ne 0 ]; then
  echo ""
  echo "Test-support packages may only be imported from _test.go files."
  exit 1
fi

echo "PASS: test-support packages are imported only by tests"
exit 0
