#!/usr/bin/env bash
# Runs the end-to-end suite and a short soak on every device in a matrix
# file, one entry at a time, and keeps the results.
#
# Usage: scripts/nightly.sh [--dry-run] <matrix-file>
#
# Matrix format, one entry per line, blank lines and lines starting with
# a hash are ignored:
#
#   <kind> <id> <soak-count>
#
# Kinds:
#   android           id is a connected adb serial, used as is
#   android-emulator  id is an AVD name, booted headless and killed at the end
#   iossim            id is a simulator UDID, booted only when not booted yet
#   ios               id is a physical device UDID
#
# soak-count is the number of soak iterations, and 0 skips the soak.
#
# Per entry the script sets the device up, runs 'make e2e', runs
# scripts/soak.sh when the count is above 0, then tears down only what
# that entry started. A failing entry is recorded and the next entry
# still runs. An iOS entry is skipped when something already listens on
# the runner port, because that runner belongs to someone else.
#
# --dry-run validates the matrix and prints the plan per entry without
# touching any device, directory, or process.
#
# Environment:
#   ATOMYX_NIGHTLY_RESULTS_DIR  parent of the run directory, default e2e-results/nightly
#   ATOMYX_BIN                  atomyx binary used for setup, default core/bin/atomyx
#   ATOMYX_SOAK_BIN             soak script, default scripts/soak.sh
#
# Exit codes:
#   0  every entry is ok
#   1  at least one entry failed or was skipped
#   2  misuse: bad arguments or an invalid matrix
#
# Results land in <results dir>/<UTC date-time>/<index>-<kind>/ with
# setup.log, e2e.log, soak.log and result.txt, plus summary.txt at the
# top. Run storage lives under the run directory, so the script never
# writes into the operator's own ~/.atomyx.
set -euo pipefail

readonly RUNNER_HOST="127.0.0.1"
readonly RUNNER_PORT="22087"
readonly RUNNER_WAIT_S=600
readonly RUNNER_RELEASE_WAIT_S=15
readonly EMULATOR_BOOT_WAIT_S=300
readonly EMULATOR_FIRST_PORT=5554
readonly EMULATOR_LAST_PORT=5584
readonly DEMO_APP_ID="dev.atomyx.demo"
readonly DEMO_REL_APK="examples/atomyx-demo/build/app/outputs/flutter-apk/app-debug.apk"
readonly DEMO_REL_SIM_APP="examples/atomyx-demo/build/ios-sim/Build/Products/Debug-iphonesimulator/Runner.app"

usage() {
  echo "usage: scripts/nightly.sh [--dry-run] <matrix-file>" >&2
  echo "  matrix line: <android|android-emulator|iossim|ios> <id> <soak-count>" >&2
  echo "  example: scripts/matrix.example" >&2
}

dry_run=0
matrix=""
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=1 ;;
    -h | --help) usage; exit 0 ;;
    -*) echo "error: unknown option ${arg}." >&2; usage; exit 2 ;;
    *)
      if [ -n "$matrix" ]; then
        echo "error: more than one matrix file given." >&2
        usage
        exit 2
      fi
      matrix="$arg"
      ;;
  esac
done

if [ -z "$matrix" ]; then
  usage
  exit 2
fi
if [ ! -f "$matrix" ]; then
  echo "error: matrix file ${matrix} does not exist." >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
atomyx_bin="${ATOMYX_BIN:-${repo_root}/core/bin/atomyx}"
soak_bin="${ATOMYX_SOAK_BIN:-${repo_root}/scripts/soak.sh}"

kinds=()
ids=()
counts=()

# Reads the whole matrix before anything runs, so a typo on the last
# line cannot leave the first devices half processed.
parse_matrix() {
  local line_no=0 line kind id count extra
  while IFS= read -r line || [ -n "$line" ]; do
    line_no=$((line_no + 1))
    case "$line" in
      '' | '#'*) continue ;;
    esac
    read -r kind id count extra <<< "$line"
    case "$kind" in
      '' | '#'*) continue ;;
    esac
    if [ -z "$id" ] || [ -z "$count" ] || [ -n "$extra" ]; then
      echo "error: ${matrix} line ${line_no}: expected '<kind> <id> <soak-count>'." >&2
      exit 2
    fi
    case "$kind" in
      android | android-emulator | iossim | ios) ;;
      *)
        echo "error: ${matrix} line ${line_no}: unknown kind '${kind}'." >&2
        exit 2
        ;;
    esac
    case "$count" in
      *[!0-9]*)
        echo "error: ${matrix} line ${line_no}: soak-count '${count}' is not a non-negative integer." >&2
        exit 2
        ;;
    esac
    kinds+=("$kind")
    ids+=("$id")
    counts+=("$count")
  done < "$matrix"
  if [ "${#kinds[@]}" -eq 0 ]; then
    echo "error: ${matrix} has no entries." >&2
    exit 2
  fi
}

parse_matrix

platform_of() {
  case "$1" in
    android | android-emulator) echo "android" ;;
    *) echo "$1" ;;
  esac
}

if [ "$dry_run" -eq 1 ]; then
  echo "nightly plan for ${matrix} (dry run, nothing is touched)"
  if [ ! -x "$atomyx_bin" ]; then
    echo "prepare: make build-core"
  fi
  for i in "${!kinds[@]}"; do
    kind="${kinds[$i]}"
    id="${ids[$i]}"
    count="${counts[$i]}"
    echo "entry $((i + 1)): ${kind} ${id} soak=${count}"
    case "$kind" in
      android-emulator)
        echo "  start: emulator -avd ${id} -no-window -no-snapshot-save -port <free port>"
        echo "  wait: adb wait-for-device, then sys.boot_completed"
        echo "  setup: core/bin/atomyx setup --platform android --device <emulator serial>"
        echo "  install: adb -s <emulator serial> install -r ${DEMO_REL_APK} (when built)"
        echo "  e2e: ATOMYX_ANDROID_SERIAL=<emulator serial> make e2e PLATFORM=android"
        [ "$count" -gt 0 ] && echo "  soak: scripts/soak.sh android <emulator serial> ${count}"
        echo "  teardown: adb -s <emulator serial> emu kill"
        ;;
      android)
        echo "  setup: core/bin/atomyx setup --platform android --device ${id}"
        echo "  install: adb -s ${id} install -r ${DEMO_REL_APK} (when built)"
        echo "  e2e: ATOMYX_ANDROID_SERIAL=${id} make e2e PLATFORM=android"
        [ "$count" -gt 0 ] && echo "  soak: scripts/soak.sh android ${id} ${count}"
        echo "  teardown: nothing started"
        ;;
      iossim)
        echo "  guard: skip when ${RUNNER_HOST}:${RUNNER_PORT} already listens"
        echo "  start: xcrun simctl boot ${id} (only when not booted)"
        echo "  setup: make -C platforms/ios-agent serve UDID=${id}, wait up to ${RUNNER_WAIT_S}s for the runner"
        echo "  install: xcrun simctl install ${id} ${DEMO_REL_SIM_APP} (when built)"
        echo "  e2e: ATOMYX_IOS_SIM_UDID=${id} make e2e PLATFORM=iossim"
        [ "$count" -gt 0 ] && echo "  soak: scripts/soak.sh iossim ${id} ${count}"
        echo "  teardown: stop the runner and its children, shut the simulator down only if this run booted it"
        ;;
      ios)
        echo "  guard: skip when ${RUNNER_HOST}:${RUNNER_PORT} already listens"
        echo "  setup: make -C platforms/ios-agent serve-device DEVICE_UDID=${id}, wait up to ${RUNNER_WAIT_S}s for the runner"
        echo "  e2e: ATOMYX_IOS_DEVICE_UDID=${id} make e2e PLATFORM=ios"
        [ "$count" -gt 0 ] && echo "  soak: scripts/soak.sh ios ${id} ${count}"
        echo "  teardown: stop the runner and its children"
        ;;
    esac
  done
  exit 0
fi

if [ ! -x "$atomyx_bin" ]; then
  echo "core/bin/atomyx is missing, building it with make build-core"
  make -C "$repo_root" build-core
fi

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
results_parent="${ATOMYX_NIGHTLY_RESULTS_DIR:-${repo_root}/e2e-results/nightly}"
run_dir="${results_parent}/${stamp}"
mkdir -p "$run_dir"
summary_file="${run_dir}/summary.txt"
: > "$summary_file"

# State of the entry in flight, read by cleanup. Each is empty when the
# entry did not start the matching resource.
runner_pid=""
emulator_pid=""
emulator_serial=""
booted_sim=""
runner_listener=""
device_id=""
fail_reason=""

kill_tree() {
  local pid="$1" child
  for child in $(pgrep -P "$pid" 2>/dev/null || true); do
    kill_tree "$child"
  done
  kill "$pid" 2>/dev/null || true
}

listening_pid() {
  lsof -ti "tcp:${RUNNER_PORT}" -sTCP:LISTEN 2>/dev/null | head -1 || true
}

# Confirms the runner port is free after a teardown. The runner inside a
# simulator is started by the simulator's own launchd, so it can outlive
# the process tree this script started. The listener recorded right after
# this entry's runner came up is the only process stopped here, and only
# while it still holds the port. Any other listener belongs to someone
# else and only produces a warning.
release_runner_port() {
  local waited=0
  while runner_listening && [ "$waited" -lt "$RUNNER_RELEASE_WAIT_S" ]; do
    sleep 1
    waited=$((waited + 1))
  done
  if runner_listening && [ -n "$runner_listener" ] && [ "$(listening_pid)" = "$runner_listener" ]; then
    kill "$runner_listener" 2>/dev/null || true
    sleep 2
  fi
  runner_listener=""
  if runner_listening; then
    echo "warning: port ${RUNNER_PORT} is still in use after teardown" >&2
  fi
}

cleanup() {
  if [ -n "$runner_pid" ]; then
    kill_tree "$runner_pid"
    wait "$runner_pid" 2>/dev/null || true
    runner_pid=""
    release_runner_port
  fi
  if [ -n "$emulator_serial" ]; then
    adb -s "$emulator_serial" emu kill >/dev/null 2>&1 || true
    emulator_serial=""
  fi
  if [ -n "$emulator_pid" ]; then
    kill "$emulator_pid" 2>/dev/null || true
    wait "$emulator_pid" 2>/dev/null || true
    emulator_pid=""
  fi
  if [ -n "$booted_sim" ]; then
    xcrun simctl shutdown "$booted_sim" >/dev/null 2>&1 || true
    booted_sim=""
  fi
}

on_signal() {
  cleanup
  echo "interrupted: yes" >> "$summary_file"
  exit "$1"
}

trap cleanup EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

runner_listening() {
  nc -z "$RUNNER_HOST" "$RUNNER_PORT" >/dev/null 2>&1
}

wait_for_runner() {
  local waited=0
  while ! runner_listening; do
    if ! kill -0 "$runner_pid" 2>/dev/null; then
      fail_reason="the runner process exited before it listened on ${RUNNER_HOST}:${RUNNER_PORT}"
      return 1
    fi
    if [ "$waited" -ge "$RUNNER_WAIT_S" ]; then
      fail_reason="the runner did not listen on ${RUNNER_HOST}:${RUNNER_PORT} within ${RUNNER_WAIT_S}s"
      return 1
    fi
    sleep 2
    waited=$((waited + 2))
  done
}

# Picks the first even console port in the emulator range that adb does
# not already list, so the serial of the emulator this script starts is
# known without scanning for it.
free_emulator_port() {
  local port listed
  listed="$(adb devices 2>/dev/null || true)"
  for ((port = EMULATOR_FIRST_PORT; port <= EMULATOR_LAST_PORT; port += 2)); do
    if ! printf '%s\n' "$listed" | grep -q "^emulator-${port}[[:space:]]"; then
      echo "$port"
      return 0
    fi
  done
  return 1
}

boot_emulator() {
  local avd="$1" log="$2" port waited=0
  if ! port="$(free_emulator_port)"; then
    fail_reason="no free emulator console port between ${EMULATOR_FIRST_PORT} and ${EMULATOR_LAST_PORT}"
    return 1
  fi
  emulator -avd "$avd" -no-window -no-snapshot-save -port "$port" >> "$log" 2>&1 &
  emulator_pid=$!
  emulator_serial="emulator-${port}"
  adb -s "$emulator_serial" wait-for-device || return 1
  until [ "$(adb -s "$emulator_serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; do
    if [ "$waited" -ge "$EMULATOR_BOOT_WAIT_S" ]; then
      fail_reason="the emulator did not finish booting within ${EMULATOR_BOOT_WAIT_S}s"
      return 1
    fi
    sleep 3
    waited=$((waited + 3))
  done
}

# A streamed install over a wireless adb link fails transiently now and
# then, so one retry separates a flaky link from a real rejection.
install_with_retry() {
  local serial="$1" log="$2" attempt
  for attempt in 1 2; do
    if adb -s "$serial" install -r "${repo_root}/${DEMO_REL_APK}" >> "$log" 2>&1; then
      return 0
    fi
    sleep 3
  done
  return 1
}

# Sets the device up and leaves the identifiers the later steps need in
# device_id. Returns non-zero and sets fail_reason when it cannot. It must
# run in the current shell, because it records the processes it starts.
prepare_entry() {
  local kind="$1" id="$2" log="$3"
  case "$kind" in
    android-emulator)
      boot_emulator "$id" "$log" || return 1
      device_id="$emulator_serial"
      ;;
    iossim)
      device_id="$id"
      if ! xcrun simctl list devices | grep "$id" | grep -q "(Booted)"; then
        if ! xcrun simctl boot "$id" >> "$log" 2>&1; then
          fail_reason="simulator boot failed"
          return 1
        fi
        booted_sim="$id"
        if ! xcrun simctl bootstatus "$id" -b >> "$log" 2>&1; then
          fail_reason="simulator did not finish booting"
          return 1
        fi
      fi
      ;;
    *) device_id="$id" ;;
  esac

  case "$kind" in
    android | android-emulator)
      "$atomyx_bin" setup --platform android --device "$device_id" >> "$log" 2>&1 || {
        fail_reason="atomyx setup failed"
        return 1
      }
      if [ -f "${repo_root}/${DEMO_REL_APK}" ]; then
        install_with_retry "$device_id" "$log" || {
          fail_reason="demo app install failed"
          return 1
        }
      fi
      ;;
    iossim)
      make -C "${repo_root}/platforms/ios-agent" serve UDID="$device_id" >> "$log" 2>&1 &
      runner_pid=$!
      wait_for_runner || return 1
      runner_listener="$(listening_pid)"
      if [ -d "${repo_root}/${DEMO_REL_SIM_APP}" ]; then
        xcrun simctl install "$device_id" "${repo_root}/${DEMO_REL_SIM_APP}" >> "$log" 2>&1 || {
          fail_reason="demo app install failed"
          return 1
        }
      fi
      ;;
    ios)
      make -C "${repo_root}/platforms/ios-agent" serve-device DEVICE_UDID="$device_id" >> "$log" 2>&1 &
      runner_pid=$!
      wait_for_runner || return 1
      runner_listener="$(listening_pid)"
      ;;
  esac
}

count_of() {
  local n
  n="$(grep -E "^$1: " "$2" 2>/dev/null | tail -1 | sed "s/^$1: //")"
  echo "${n:-n/a}"
}

# Counts the offenders the skip gate printed. A log without a gate verdict
# line means the leg never reached the gate, so the count is unknown.
unexpected_skips_of() {
  if grep -q "^skip-gate: " "$1" 2>/dev/null; then
    grep -c "^unexpected-skip: " "$1" || true
  else
    echo "n/a"
  fi
}

all_ok=1
for i in "${!kinds[@]}"; do
  kind="${kinds[$i]}"
  id="${ids[$i]}"
  count="${counts[$i]}"
  platform="$(platform_of "$kind")"
  index=$((i + 1))
  entry_dir="${run_dir}/${index}-${kind}"
  mkdir -p "$entry_dir"
  setup_log="${entry_dir}/setup.log"
  e2e_log="${entry_dir}/e2e.log"
  soak_log="${entry_dir}/soak.log"
  : > "$setup_log"
  : > "$e2e_log"
  : > "$soak_log"
  export ATOMYX_STORAGE_DIR="${entry_dir}/storage"

  status="ok"
  reason=""
  e2e_exit="n/a"
  soak_exit="n/a"
  device_id=""
  fail_reason=""

  echo "entry ${index}: ${kind} ${id} soak=${count}"

  if { [ "$platform" = "ios" ] || [ "$platform" = "iossim" ]; } && runner_listening; then
    status="skipped"
    reason="port ${RUNNER_PORT} is already in use by another runner"
  else
    if ! prepare_entry "$kind" "$id" "$setup_log"; then
      status="failed"
      reason="${fail_reason:-device setup failed}"
    fi
  fi

  if [ "$status" = "ok" ]; then
    set +e
    case "$kind" in
      android | android-emulator)
        ATOMYX_ANDROID_SERIAL="$device_id" \
          ATOMYX_ANDROID_INSTALL_APK="${repo_root}/${DEMO_REL_APK}" \
          ATOMYX_ANDROID_INSTALL_APP_ID="$DEMO_APP_ID" \
          make -C "$repo_root" e2e PLATFORM=android > "$e2e_log" 2>&1
        ;;
      iossim)
        ATOMYX_IOS_SIM_UDID="$device_id" \
          ATOMYX_IOS_INSTALL_APP="${repo_root}/${DEMO_REL_SIM_APP}" \
          ATOMYX_IOS_INSTALL_APP_ID="$DEMO_APP_ID" \
          make -C "$repo_root" e2e PLATFORM=iossim > "$e2e_log" 2>&1
        ;;
      ios)
        ATOMYX_IOS_DEVICE_UDID="$device_id" \
          make -C "$repo_root" e2e PLATFORM=ios > "$e2e_log" 2>&1
        ;;
    esac
    e2e_exit=$?
    if [ "$count" -gt 0 ]; then
      ATOMYX_SOAK_RESULTS_DIR="${entry_dir}/soak" \
        "$soak_bin" "$platform" "$device_id" "$count" > "$soak_log" 2>&1
      soak_exit=$?
    fi
    set -e
    if [ "$e2e_exit" -ne 0 ] || { [ "$soak_exit" != "n/a" ] && [ "$soak_exit" -ne 0 ]; }; then
      status="failed"
    fi
  fi

  soak_summary="$(ls "${entry_dir}"/soak/*/summary.txt 2>/dev/null | head -1 || true)"
  {
    echo "kind: ${kind}"
    echo "id: ${id}"
    echo "e2e exit: ${e2e_exit}"
    echo "e2e pass: $(count_of pass "$e2e_log")"
    echo "e2e fail: $(count_of fail "$e2e_log")"
    echo "e2e skip: $(count_of skip "$e2e_log")"
    echo "e2e unexpectedSkips: $(unexpected_skips_of "$e2e_log")"
    echo "soak exit: ${soak_exit}"
    echo "soak passed: $(count_of passed "${soak_summary:-/dev/null}")"
    echo "soak failed: $(count_of failed "${soak_summary:-/dev/null}")"
    echo "soak infraFailures: $(count_of infraFailures "${soak_summary:-/dev/null}")"
    echo "soak infraFailureRate: $(count_of infraFailureRate "${soak_summary:-/dev/null}")"
    echo "status: ${status}"
    [ -z "$reason" ] || echo "reason: ${reason}"
  } > "${entry_dir}/result.txt"

  cleanup

  line="${index} ${kind} ${id} ${status}"
  [ -z "$reason" ] || line="${line} (${reason})"
  echo "$line" | tee -a "$summary_file"
  [ "$status" = "ok" ] || all_ok=0
done

echo "results: ${run_dir}"
[ "$all_ok" -eq 1 ]
