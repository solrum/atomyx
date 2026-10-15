#!/usr/bin/env bash
#
# Behavior test for scripts/e2e-trend.sh. It builds fixture result
# folders under a temporary directory and asserts the tables, the
# summary lines, the pooled infrastructure failure rate, and the exit
# codes. It needs no device.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
trend="$root/scripts/e2e-trend.sh"

work="$(mktemp -d "${TMPDIR:-/tmp}/e2e-trend-check.XXXXXX")"
trap 'rm -rf "$work"' EXIT

failures=0

fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

# entry NIGHT INDEX KIND ID FIELDS...
# Writes result.txt with the given "key: value" lines.
entry() {
  local night="$1" index="$2" kind="$3" id="$4" dir
  shift 4
  dir="$work/results/$night/$index-$kind"
  mkdir -p "$dir"
  {
    echo "kind: $kind"
    echo "id: $id"
    printf '%s\n' "$@"
  } > "$dir/result.txt"
}

# The oldest night is outside the three-night window and carries a
# huge failure count that would show up in the pooled rate if it leaked.
entry 20260101T000000Z 1 android SER-A "e2e pass: 1" "e2e fail: 0" "e2e skip: 0" \
  "e2e unexpectedSkips: 0" "soak passed: 0" "soak failed: 100" "soak infraFailures: 100" \
  "soak infraFailureRate: 1.000000" "status: failed"

entry 20260102T000000Z 1 android SER-A "e2e pass: 10" "e2e fail: 0" "e2e skip: 1" \
  "e2e unexpectedSkips: 0" "soak passed: 4" "soak failed: 1" "soak infraFailures: 1" \
  "soak infraFailureRate: 0.200000" "status: ok"
entry 20260102T000000Z 2 iossim SIM-B "e2e pass: 20" "e2e fail: 0" "e2e skip: 2" \
  "e2e unexpectedSkips: 0" "soak passed: 2" "soak failed: 0" "soak infraFailures: 0" \
  "soak infraFailureRate: 0.000000" "status: ok"

# Android recorded nothing on this night, and iossim is absent.
entry 20260103T000000Z 1 android SER-A "e2e pass: n/a" "e2e fail: n/a" "e2e skip: n/a" \
  "e2e unexpectedSkips: n/a" "soak passed: n/a" "soak failed: n/a" "soak infraFailures: n/a" \
  "soak infraFailureRate: n/a" "status: failed" "reason: demo app install failed"

# On this night iossim has no unexpected skip count and ios carries only a
# failure rate, so the report must print n/a and derive the pooled rate.
entry 20260104T000000Z 1 android SER-A "e2e pass: 9" "e2e fail: 0" "e2e skip: 1" \
  "e2e unexpectedSkips: 2" "soak passed: 9" "soak failed: 1" "soak infraFailures: 0" \
  "soak infraFailureRate: 0.000000" "status: ok"
entry 20260104T000000Z 2 iossim SIM-B "e2e pass: 20" "e2e fail: 0" "e2e skip: 2" \
  "soak passed: 3" "soak failed: 0" "soak infraFailureRate: 0.000000" "status: ok"
entry 20260104T000000Z 3 ios PHONE-C "e2e pass: 5" "e2e fail: 0" "e2e skip: 0" \
  "e2e unexpectedSkips: 0" "soak passed: 1" "soak failed: 1" "soak infraFailureRate: 0.500000" \
  "status: failed"

output="$(ATOMYX_NIGHTLY_RESULTS_DIR="$work/results" "$trend" --nights 3 2>&1)"

expect_text() {
  if ! printf '%s\n' "$output" | grep -qF -- "$1"; then
    fail "output lacks '$1'"
  fi
}

reject_text() {
  if printf '%s\n' "$output" | grep -qF -- "$1"; then
    fail "output has unexpected '$1'"
  fi
}

expect_text "device android SER-A"
expect_text "device iossim SIM-B"
expect_text "device ios PHONE-C"
reject_text "20260101T000000Z"

# Android: two of three nights are covered by the pooled rate, the
# night with n/a values adds nothing, and the older night is excluded.
expect_text "summary: green nights 2 of 3, soak runs 15, pooled infra failure rate 0.066667"
# Iossim is absent on the middle night, so it has two rows and no n/a row.
expect_text "summary: green nights 2 of 2, soak runs 5, pooled infra failure rate 0.000000"
# The rate-only result still contributes through its rate.
expect_text "summary: green nights 0 of 1, soak runs 2, pooled infra failure rate 0.500000"

android_rows="$(printf '%s\n' "$output" | awk '/^device android/{f=1;next} /^device /{f=0} f && /^2026/')"
[ "$(printf '%s\n' "$android_rows" | wc -l | tr -d ' ')" -eq 3 ] || fail "android table does not have three rows"
printf '%s\n' "$android_rows" | grep -q '^20260103T000000Z .*n/a.*failed$' || fail "android n/a night row is wrong"
printf '%s\n' "$android_rows" | grep -q '^20260104T000000Z .* 2 .*ok$' || fail "android unexpected skip count missing"

iossim_rows="$(printf '%s\n' "$output" | awk '/^device iossim/{f=1;next} /^device /{f=0} f && /^2026/')"
[ "$(printf '%s\n' "$iossim_rows" | wc -l | tr -d ' ')" -eq 2 ] || fail "iossim table does not have two rows"
if printf '%s\n' "$iossim_rows" | grep -q '^20260103T000000Z'; then
  fail "iossim has a row for a night it was absent"
fi
printf '%s\n' "$iossim_rows" | grep -q '^20260104T000000Z .*n/a.* ok$' || fail "iossim missing unexpected skip count is not n/a"

# The default window of 14 nights covers every fixture night.
default_output="$(ATOMYX_NIGHTLY_RESULTS_DIR="$work/results" "$trend")"
printf '%s\n' "$default_output" | grep -qF "20260101T000000Z" || fail "default window drops the oldest night"
printf '%s\n' "$default_output" | grep -qF "soak runs 115," || fail "default window pooled soak runs are wrong"

# The results directory flag works without the environment variable.
flag_output="$("$trend" --nights 1 --results-dir "$work/results")"
printf '%s\n' "$flag_output" | grep -qF "device ios PHONE-C" || fail "--results-dir output lacks the device"
if printf '%s\n' "$flag_output" | grep -qF "20260103T000000Z"; then
  fail "--nights 1 prints more than the last night"
fi

check_exit() {
  local name="$1" want="$2" status
  shift 2
  set +e
  "$@" > /dev/null 2>&1
  status=$?
  set -e
  [ "$status" -eq "$want" ] || fail "$name: exit $status, want $want"
}

check_exit "missing directory" 2 "$trend" --results-dir "$work/absent"
check_exit "zero nights" 2 "$trend" --nights 0 --results-dir "$work/results"
check_exit "non-numeric nights" 2 "$trend" --nights many --results-dir "$work/results"
check_exit "missing value" 2 "$trend" --nights
check_exit "unknown option" 2 "$trend" --bogus
mkdir -p "$work/empty"
check_exit "empty directory" 0 "$trend" --results-dir "$work/empty"

if [ "$failures" -ne 0 ]; then
  echo "e2e trend check: $failures failure(s)" >&2
  exit 1
fi
echo "e2e trend check: pass"
