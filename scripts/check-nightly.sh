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

# Real runs against fake tools. The fakes record their argv and the
# device variables they receive, so a lost device id fails the check.
fakes="$work/fakes"
mkdir -p "$fakes"
export FAKE_LOG="$work/fake.log"
cat > "$fakes/atomyx" <<'FAKE'
#!/usr/bin/env bash
echo "atomyx $*" >> "$FAKE_LOG"
FAKE
cat > "$fakes/adb" <<'FAKE'
#!/usr/bin/env bash
echo "adb $*" >> "$FAKE_LOG"
FAKE
cat > "$fakes/xcrun" <<'FAKE'
#!/usr/bin/env bash
echo "xcrun $*" >> "$FAKE_LOG"
if [ "$2" = "list" ]; then echo "    Fake (SIM-1) (Booted)"; fi
FAKE
cat > "$fakes/nc" <<'FAKE'
#!/usr/bin/env bash
[ -e "$FAKE_RUNNER_FLAG" ]
FAKE
cat > "$fakes/lsof" <<'FAKE'
#!/usr/bin/env bash
[ -e "$FAKE_RUNNER_FLAG" ] && cat "$FAKE_RUNNER_FLAG"
exit 0
FAKE
cat > "$fakes/make" <<'FAKE'
#!/usr/bin/env bash
echo "make $*" >> "$FAKE_LOG"
case "$*" in
  *" e2e "* | *" e2e")
    echo "env ATOMYX_ANDROID_SERIAL=${ATOMYX_ANDROID_SERIAL:-} ATOMYX_IOS_SIM_UDID=${ATOMYX_IOS_SIM_UDID:-} ATOMYX_IOS_DEVICE_UDID=${ATOMYX_IOS_DEVICE_UDID:-}" >> "$FAKE_LOG"
    echo "pass: 7"; echo "fail: 0"; echo "skip: 2"; echo "skip-gate: pass"
    ;;
  *serve-device*)
    if [ "${FAKE_DEVICE_SERVE:-print}" = "print" ]; then
      echo "[atomyx] driver listening on 127.0.0.1:22087"
    fi
    echo $$ > "$FAKE_DEVICE_PID"
    trap 'rm -f "$FAKE_DEVICE_PID"; exit 0' TERM
    sleep 600 &
    wait
    ;;
  *serve*)
    echo 4242 > "$FAKE_RUNNER_FLAG"
    trap 'rm -f "$FAKE_RUNNER_FLAG"; exit 0' TERM
    sleep 600 &
    wait
    ;;
esac
FAKE
cat > "$fakes/soak.sh" <<'FAKE'
#!/usr/bin/env bash
echo "soak $*" >> "$FAKE_LOG"
mkdir -p "$ATOMYX_SOAK_RESULTS_DIR/r"
printf 'passed: 3\nfailed: 0\ninfraFailures: 0\ninfraFailureRate: 0.000000\n' > "$ATOMYX_SOAK_RESULTS_DIR/r/summary.txt"
FAKE
chmod +x "$fakes"/*
export FAKE_RUNNER_FLAG="$work/runner.flag"
export FAKE_DEVICE_PID="$work/device.pid"

run_real() {
  local name="$1" content="$2"
  printf '%s' "$content" > "$work/$name.matrix"
  : > "$FAKE_LOG"
  rm -f "$FAKE_RUNNER_FLAG"
  set +e
  PATH="$fakes:$PATH" ATOMYX_BIN="$fakes/atomyx" ATOMYX_SOAK_BIN="$fakes/soak.sh" \
    ATOMYX_NIGHTLY_RESULTS_DIR="$work/real-$name" \
    "$nightly" "$work/$name.matrix" > "$work/real-$name.out" 2>&1
  real_status=$?
  set -e
  real_dir="$(ls -d "$work/real-$name"/*/ 2>/dev/null | head -1)"
}

expect_log() {
  if ! grep -qF -- "$2" "$FAKE_LOG"; then
    fail "$1: fake log lacks '$2'"
  fi
}

run_real android $'android SERIAL-A 2\n'
[ "$real_status" -eq 0 ] || fail "real android: exit $real_status, want 0"
expect_log "real android" "env ATOMYX_ANDROID_SERIAL=SERIAL-A"
expect_log "real android" "soak android SERIAL-A 2"
expect_log "real android" "atomyx setup --platform android --device SERIAL-A"
for line in "e2e exit: 0" "e2e pass: 7" "e2e skip: 2" "e2e unexpectedSkips: 0" "soak passed: 3" "soak infraFailures: 0" "status: ok"; do
  grep -qx -- "$line" "${real_dir}1-android/result.txt" || fail "real android: result.txt lacks '$line'"
done

run_real iossim $'iossim SIM-1 1\n'
[ "$real_status" -eq 0 ] || fail "real iossim: exit $real_status, want 0"
expect_log "real iossim" "env ATOMYX_ANDROID_SERIAL= ATOMYX_IOS_SIM_UDID=SIM-1 "
expect_log "real iossim" "soak iossim SIM-1 1"
if [ -e "$FAKE_RUNNER_FLAG" ]; then
  fail "real iossim: the runner was not torn down"
fi

# A physical device runner has no host listener, so readiness is the
# runner's announcement in the setup log.
export ATOMYX_NIGHTLY_RUNNER_WAIT_S=4
rm -f "$FAKE_DEVICE_PID"
FAKE_DEVICE_SERVE=print run_real iosready $'ios DEV-1 1\n'
[ "$real_status" -eq 0 ] || fail "real ios ready: exit $real_status, want 0"
expect_log "real ios ready" "serve-device DEVICE_UDID=DEV-1"
expect_log "real ios ready" "ATOMYX_IOS_DEVICE_UDID=DEV-1"
expect_log "real ios ready" "soak ios DEV-1 1"
if [ -e "$FAKE_DEVICE_PID" ]; then
  fail "real ios ready: the serve process was not torn down"
fi

FAKE_DEVICE_SERVE=silent run_real iossilent $'ios DEV-1 1\n'
[ "$real_status" -eq 1 ] || fail "real ios silent: exit $real_status, want 1"
grep -q "did not report 'driver listening' within 4s" "$work/real-iossilent.out" \
  || fail "real ios silent: no readiness timeout reported"
if grep -q ' e2e ' "$FAKE_LOG"; then
  fail "real ios silent: e2e ran although the runner never became ready"
fi
if [ -e "$FAKE_DEVICE_PID" ]; then
  fail "real ios silent: the serve process was not torn down"
fi
unset ATOMYX_NIGHTLY_RUNNER_WAIT_S

# A runner that already listens makes the iOS entry skipped, and a
# failing e2e leg is recorded without stopping the next entry.
: > "$FAKE_LOG"
touch "$FAKE_RUNNER_FLAG"
printf 'iossim SIM-1 0\n' > "$work/busy.matrix"
set +e
PATH="$fakes:$PATH" ATOMYX_BIN="$fakes/atomyx" ATOMYX_SOAK_BIN="$fakes/soak.sh" \
  ATOMYX_NIGHTLY_RESULTS_DIR="$work/real-busy" "$nightly" "$work/busy.matrix" > "$work/real-busy.out" 2>&1
busy_status=$?
set -e
rm -f "$FAKE_RUNNER_FLAG"
[ "$busy_status" -eq 1 ] || fail "busy runner: exit $busy_status, want 1"
grep -q 'skipped' "$work/real-busy.out" || fail "busy runner: entry not reported as skipped"

if [ "$failures" -ne 0 ]; then
  echo "nightly check: $failures failure(s)" >&2
  exit 1
fi
echo "nightly check: pass"
