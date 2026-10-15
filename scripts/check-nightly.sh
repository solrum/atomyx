#!/usr/bin/env bash
#
# Behavior test for scripts/nightly.sh. It needs no device. It runs the
# dry run against the committed example matrix and against malformed
# matrices, and asserts the exit codes and the plan text. It also checks
# that a dry run creates no result directory.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
nightly="$root/scripts/nightly.sh"

work="$(mktemp -d "${TMPDIR:-/tmp}/nightly-check.XXXXXX")"
trap 'rm -rf "$work"' EXIT

failures=0

fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

# run_case NAME EXPECTED_STATUS MATRIX_CONTENT
# Leaves the combined output in $output.
run_case() {
  local name="$1" want="$2" content="$3" status
  printf '%s' "$content" > "$work/$name.matrix"
  set +e
  output="$(ATOMYX_NIGHTLY_RESULTS_DIR="$work/results" "$nightly" --dry-run "$work/$name.matrix" 2>&1)"
  status=$?
  set -e
  if [ "$status" -ne "$want" ]; then
    fail "$name: exit $status, want $want"
  fi
}

expect_text() {
  if ! printf '%s\n' "$output" | grep -qF -- "$2"; then
    fail "$1: output lacks '$2'"
  fi
}

set +e
output="$(ATOMYX_NIGHTLY_RESULTS_DIR="$work/results" "$nightly" --dry-run "$root/scripts/matrix.example" 2>&1)"
status=$?
set -e
[ "$status" -eq 0 ] || fail "example matrix: exit $status, want 0"
expect_text "example matrix" "entry 1: android "
expect_text "example matrix" "entry 2: android-emulator "
expect_text "example matrix" "entry 3: iossim "
expect_text "example matrix" "entry 4: ios "
expect_text "example matrix" "make e2e PLATFORM=android"
expect_text "example matrix" "make e2e PLATFORM=iossim"
expect_text "example matrix" "make e2e PLATFORM=ios"
expect_text "example matrix" "emulator -avd"
expect_text "example matrix" "serve-device"
expect_text "example matrix" "only when not booted"
expect_text "example matrix" "already listens"

if [ -e "$work/results" ]; then
  fail "dry run created the results directory"
fi

run_case zero 0 $'android SERIAL-X 0\n'
if printf '%s\n' "$output" | grep -q 'soak:'; then
  fail "zero: a soak line is planned for soak-count 0"
fi

run_case comments 0 $'# header\n\n   \nandroid SERIAL-X 3\n# trailing\n'
expect_text "comments" "entry 1: android SERIAL-X soak=3"

run_case unknown 2 $'windows DEVICE-X 3\n'
expect_text "unknown kind" "unknown kind 'windows'"

run_case missing 2 $'android SERIAL-X\n'
expect_text "missing field" "expected '<kind> <id> <soak-count>'"

run_case extra 2 $'android SERIAL-X 3 more\n'
expect_text "extra field" "expected '<kind> <id> <soak-count>'"

run_case nonnumeric 2 $'ios DEVICE-X many\n'
expect_text "non-numeric count" "not a non-negative integer"

run_case negative 2 $'ios DEVICE-X -1\n'
expect_text "negative count" "not a non-negative integer"

run_case empty 2 $'# only a comment\n'
expect_text "empty matrix" "has no entries"

run_case lateerror 2 $'android SERIAL-X 3\nbogus ID 1\n'
if printf '%s\n' "$output" | grep -q '^entry'; then
  fail "late error: a plan was printed before the matrix was fully validated"
fi

set +e
"$nightly" --dry-run "$work/absent.matrix" > /dev/null 2>&1
status=$?
set -e
[ "$status" -eq 2 ] || fail "absent file: exit $status, want 2"

set +e
"$nightly" > /dev/null 2>&1
status=$?
set -e
[ "$status" -eq 2 ] || fail "no arguments: exit $status, want 2"

set +e
"$nightly" --bogus "$root/scripts/matrix.example" > /dev/null 2>&1
status=$?
set -e
[ "$status" -eq 2 ] || fail "unknown option: exit $status, want 2"

if [ "$failures" -ne 0 ]; then
  echo "nightly check: $failures failure(s)" >&2
  exit 1
fi
echo "nightly check: pass"
