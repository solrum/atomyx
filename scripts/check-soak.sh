#!/usr/bin/env bash
#
# Behavior test for scripts/soak.sh. It puts a fake atomyx binary in
# front of the script that replays a scripted sequence of outcomes
# (pass, fail for a test failure, infra for an infrastructure failure)
# and reports statistics in the shape of `atomyx runs stats --json`. It
# asserts the summary counts, the infrastructure rate, the threshold
# exit code, the continue-after-failure behavior, the storage default,
# the partial summary on interruption, and the argument checks.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
soak="$root/scripts/soak.sh"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failures=0

fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

fake="$work/atomyx"
cat > "$fake" <<'FAKE'
#!/usr/bin/env bash
# Fake atomyx. FAKE_OUTCOMES is a space separated list consumed one
# word per run. FAKE_STATE holds the consumed count and the history.
set -eu
state="${FAKE_STATE:?}"
touch "$state.history"
case "${1:-}" in
  run)
    n="$(wc -l < "$state.history" | tr -d ' ')"
    set -- $FAKE_OUTCOMES
    idx=$((n + 1))
    outcome="pass"
    if [ "$idx" -le "$#" ]; then
      eval "outcome=\${$idx}"
    fi
    echo "$outcome" >> "$state.history"
    echo "storage: ${ATOMYX_STORAGE_DIR:-unset}" > "$state.storage"
    if [ -n "${FAKE_RUN_SLEEP:-}" ]; then sleep "$FAKE_RUN_SLEEP"; fi
    [ "$outcome" = "pass" ] && exit 0
    exit 1
    ;;
  runs)
    if [ -n "${FAKE_STATS_FAIL:-}" ]; then exit 1; fi
    attempts="$(wc -l < "$state.history" | tr -d ' ')"
    infra="$(grep -c '^infra$' "$state.history" || true)"
    rate="$(awk -v i="$infra" -v a="$attempts" 'BEGIN { printf "%.6f", (a > 0) ? i / a : 0 }')"
    cat <<JSON
{
  "tests": [
    {"testKey": "demo", "infraFailures": 99, "attempts": 99, "infraFailureRate": 1}
  ],
  "skipped": 0,
  "infraFailures": ${infra},
  "testFailures": 0,
  "infraFailureRate": ${rate},
  "abortedRuns": 0,
  "attempts": ${attempts}
}
JSON
    ;;
  *) exit 64 ;;
esac
FAKE
chmod +x "$fake"

# run_soak NAME OUTCOMES COUNT [extra env assignments are exported by the caller]
# Leaves the exit status in $status and the summary text in $summary.
run_soak() {
  local name="$1" outcomes="$2" count="$3" max="${4:-}" results
  results="$work/results-$name"
  rm -f "$work/state.history" "$work/state.storage"
  set +e
  FAKE_STATE="$work/state" FAKE_OUTCOMES="$outcomes" ATOMYX_BIN="$fake" \
    ATOMYX_SOAK_RESULTS_DIR="$results" \
    "$soak" android serial-1 "$count" $max > "$work/out-$name.txt" 2>&1
  status=$?
  set -e
  summary="$(cat "$results"/*/summary.txt 2>/dev/null || true)"
}

expect_line() {
  if ! printf '%s\n' "$summary" | grep -qx -- "$2"; then
    fail "$1: summary lacks '$2'"
  fi
}

expect_status() {
  if [ "$status" -ne "$2" ]; then
    fail "$1: exit $status, want $2"
  fi
}

unset ATOMYX_STORAGE_DIR ATOMYX_SOAK_MAX_INFRA_RATE || true

run_soak allpass "pass pass pass pass" 4
expect_status "all pass" 0
expect_line "all pass" "total: 4"
expect_line "all pass" "passed: 4"
expect_line "all pass" "failed: 0"
expect_line "all pass" "infraFailures: 0"
expect_line "all pass" "infraFailureRate: 0.000000"

run_soak testfail "pass fail pass fail pass" 5
expect_status "test failures do not trip the infra threshold" 0
expect_line "test failures" "total: 5"
expect_line "test failures" "passed: 3"
expect_line "test failures" "failed: 2"
expect_line "test failures" "infraFailures: 0"

run_soak infra "pass infra pass pass pass pass pass pass pass pass" 10
expect_status "one infra in ten exceeds the default" 1
expect_line "infra" "total: 10"
expect_line "infra" "failed: 1"
expect_line "infra" "infraFailures: 1"
expect_line "infra" "attempts: 10"
expect_line "infra" "infraFailureRate: 0.100000"

run_soak infraok "pass infra pass pass pass" 5 "0.5"
expect_status "rate under an explicit threshold passes" 0

run_soak infrabad "pass infra pass pass pass" 5 "0.1"
expect_status "rate over an explicit threshold fails" 1

set +e
FAKE_STATE="$work/state" FAKE_OUTCOMES="pass infra" ATOMYX_BIN="$fake" \
  ATOMYX_SOAK_RESULTS_DIR="$work/results-env" ATOMYX_SOAK_MAX_INFRA_RATE=0.9 \
  "$soak" android serial-1 2 > /dev/null 2>&1
env_status=$?
set -e
rm -f "$work/state.history"
if [ "$env_status" -ne 0 ]; then
  fail "threshold from the environment: exit $env_status, want 0"
fi

run_soak continue "fail fail pass" 3
expect_line "continues after a failed iteration" "total: 3"
if [ "$(wc -l < "$work"/results-continue/*/iterations.txt | tr -d ' ')" -ne 3 ]; then
  fail "iterations.txt does not record every iteration"
fi
if ! grep -qx '1 1' "$work"/results-continue/*/iterations.txt; then
  fail "iterations.txt lacks the exit code of a failed iteration"
fi

if ! grep -q "storage: $work/results-continue/.*/storage" "$work/state.storage"; then
  fail "default storage dir is not inside the report dir"
fi

rm -f "$work/state.history"
set +e
FAKE_STATE="$work/state" FAKE_OUTCOMES="pass" ATOMYX_BIN="$fake" \
  ATOMYX_SOAK_RESULTS_DIR="$work/results-custom" ATOMYX_STORAGE_DIR="$work/custom-storage" \
  "$soak" android serial-1 1 > /dev/null 2>&1
set -e
if ! grep -qx "storage: $work/custom-storage" "$work/state.storage"; then
  fail "ATOMYX_STORAGE_DIR from the environment was not honored"
fi

run_soak nostats "pass" 1
rm -f "$work/state.history"
set +e
FAKE_STATE="$work/state" FAKE_OUTCOMES="pass" ATOMYX_BIN="$fake" FAKE_STATS_FAIL=1 \
  ATOMYX_SOAK_RESULTS_DIR="$work/results-nostats2" \
  "$soak" android serial-1 1 > /dev/null 2>&1
nostats_status=$?
set -e
if [ "$nostats_status" -ne 1 ]; then
  fail "unreadable statistics: exit $nostats_status, want 1"
fi

# Interruption: the fake sleeps so the signal lands during run 1, the
# script then stops after that run and writes a partial summary.
rm -f "$work/state.history"
FAKE_STATE="$work/state" FAKE_OUTCOMES="pass pass pass pass" ATOMYX_BIN="$fake" \
  FAKE_RUN_SLEEP=1 ATOMYX_SOAK_RESULTS_DIR="$work/results-int" \
  "$soak" android serial-1 4 > /dev/null 2>&1 &
soak_pid=$!
sleep 0.5
kill -TERM "$soak_pid"
set +e
wait "$soak_pid"
int_status=$?
set -e
summary="$(cat "$work"/results-int/*/summary.txt 2>/dev/null || true)"
if [ "$int_status" -ne 130 ]; then
  fail "interrupted run: exit $int_status, want 130"
fi
expect_line "interrupted run" "interrupted: yes"
expect_line "interrupted run" "requested: 4"
expect_line "interrupted run" "total: 1"

for bad in "" "windows" ; do
  set +e
  "$soak" "$bad" serial-1 3 > /dev/null 2>&1
  bad_status=$?
  set -e
  [ "$bad_status" -eq 2 ] || fail "platform '$bad': exit $bad_status, want 2"
done
for bad_count in 0 x "" -1; do
  set +e
  ATOMYX_BIN="$fake" "$soak" android serial-1 "$bad_count" > /dev/null 2>&1
  bad_status=$?
  set -e
  [ "$bad_status" -eq 2 ] || fail "count '$bad_count': exit $bad_status, want 2"
done
set +e
ATOMYX_BIN="$work/missing" "$soak" android serial-1 3 > /dev/null 2>&1
bad_status=$?
set -e
[ "$bad_status" -eq 2 ] || fail "missing binary: exit $bad_status, want 2"

if [ "$failures" -ne 0 ]; then
  echo "soak check: $failures failure(s)" >&2
  exit 1
fi
echo "soak check: pass"
