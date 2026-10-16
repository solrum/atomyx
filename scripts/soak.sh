#!/usr/bin/env bash
# Runs the demo soak flow repeatedly on one device and reports how often
# the infrastructure failed.
#
# Usage: scripts/soak.sh <android|ios|iossim> <device-id> <count> [max-infra-rate]
#
# Environment:
#   ATOMYX_BIN                    path to the atomyx binary, default core/bin/atomyx
#   ATOMYX_STORAGE_DIR            run storage root, default <report dir>/storage
#   ATOMYX_SOAK_MAX_INFRA_RATE    infrastructure failure rate above which the
#                                 script fails, default 0.005 (0.5 percent)
#   ATOMYX_SOAK_RESULTS_DIR       parent of the report dir, default e2e-results/soak
#
# Exit codes:
#   0    every iteration ran and the infrastructure failure rate is within the limit
#   1    the rate exceeds the limit, or the statistics could not be read
#   2    misuse: bad arguments, or the atomyx binary is missing
#   130  interrupted, after writing a partial summary
#
# Before every iteration the harness force-stops the demo app on the
# device (android and iossim), so a failed iteration cannot leave the app
# on a screen that breaks the next one. A stop that fails counts as an
# infrastructure failure of that iteration, is recorded with exit code
# 125, and no run is attempted. A failed run that left no run folder
# counts as an infrastructure failure too, because the run statistics
# never see it. The ios leg has no host tool that stops an
# app by bundle id, so it relies on the flow's own precondition alone.
# A failed iteration never stops the loop. The report lands in
# <results dir>/<UTC timestamp>-<platform>/ with one log per iteration,
# iterations.txt (iteration number and exit code per line), and
# summary.txt. Run storage defaults to a folder inside the report dir so
# a soak never mixes with the operator's own runs in ~/.atomyx.
set -euo pipefail

readonly DEFAULT_MAX_INFRA_RATE="0.005"

usage() {
  echo "usage: scripts/soak.sh <android|ios|iossim> <device-id> <count> [max-infra-rate]" >&2
  echo "  android  needs the agent ready on the device and dev.atomyx.demo installed" >&2
  echo "  ios      needs the runner served for the device and dev.atomyx.demo installed" >&2
  echo "  iossim   needs the runner served for the simulator and dev.atomyx.demo installed" >&2
}

platform="${1:-}"
device="${2:-}"
count="${3:-}"
max_rate="${4:-${ATOMYX_SOAK_MAX_INFRA_RATE:-$DEFAULT_MAX_INFRA_RATE}}"

case "$platform" in
  android) cli_platform="android" ;;
  ios | iossim) cli_platform="ios" ;;
  *) usage; exit 2 ;;
esac

if [ -z "$device" ]; then
  echo "error: device id is required." >&2
  usage
  exit 2
fi

case "$count" in
  '' | *[!0-9]* | 0)
    echo "error: count must be a positive whole number." >&2
    usage
    exit 2
    ;;
esac

if ! printf '%s\n' "$max_rate" | grep -Eq '^[0-9]*\.?[0-9]+$'; then
  echo "error: max infrastructure rate must be a number such as 0.005." >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin="${ATOMYX_BIN:-${repo_root}/core/bin/atomyx}"
flow="${repo_root}/examples/soak/demo-soak.yml"
app_id="$(sed -n 's/^appId:[[:space:]]*//p' "$flow" | head -n 1 | tr -d '"[:space:]')"
if [ -z "$app_id" ]; then
  echo "error: no appId found in ${flow}." >&2
  exit 2
fi

if [ ! -x "$bin" ]; then
  echo "error: atomyx binary not found at ${bin}." >&2
  echo "Build it with 'make build-core' from the repository root, or set ATOMYX_BIN." >&2
  exit 2
fi

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
results_root="${ATOMYX_SOAK_RESULTS_DIR:-${repo_root}/e2e-results/soak}"
out_dir="${results_root}/${stamp}-${platform}"
mkdir -p "$out_dir"
export ATOMYX_STORAGE_DIR="${ATOMYX_STORAGE_DIR:-${out_dir}/storage}"
mkdir -p "$ATOMYX_STORAGE_DIR"
: > "${out_dir}/iterations.txt"

interrupted=0
on_signal() { interrupted=1; }
trap on_signal INT TERM

total=0
passed=0
failed=0
reset_failures=0
unrecorded_failures=0
readonly RESET_FAILED_CODE=125

# Stops the app under test so the next run starts from a cold launch.
# Returns non-zero when the stop itself failed. An app that was not
# running is not a failure.
reset_app() {
  local out status
  case "$platform" in
    android)
      adb -s "$device" shell am force-stop "$app_id" > "$1" 2>&1
      ;;
    iossim)
      set +e
      out="$(xcrun simctl terminate "$device" "$app_id" 2>&1)"
      status=$?
      set -e
      printf '%s\n' "$out" > "$1"
      if [ "$status" -ne 0 ] && ! printf '%s\n' "$out" | grep -qi "nothing to terminate"; then
        return 1
      fi
      ;;
    *)
      : > "$1"
      ;;
  esac
}

echo "Soak: ${count} runs on ${platform}, report in ${out_dir}"

i=1
while [ "$i" -le "$count" ] && [ "$interrupted" -eq 0 ]; do
  log="${out_dir}/iter-$(printf '%04d' "$i").log"
  set +e
  reset_app "${out_dir}/reset-$(printf '%04d' "$i").log"
  reset_status=$?
  set -e
  if [ "$reset_status" -ne 0 ]; then
    reset_failures=$((reset_failures + 1))
    failed=$((failed + 1))
    total=$((total + 1))
    echo "${i} ${RESET_FAILED_CODE}" >> "${out_dir}/iterations.txt"
    echo "run ${i}/${count}: app reset failed"
    i=$((i + 1))
    continue
  fi
  set +e
  "$bin" run --file "$flow" --platform "$cli_platform" --device "$device" > "$log" 2>&1
  code=$?
  set -e
  total=$((total + 1))
  if [ "$code" -eq 0 ]; then
    passed=$((passed + 1))
  else
    failed=$((failed + 1))
    if ! grep -q '^Run folder: ' "$log"; then
      unrecorded_failures=$((unrecorded_failures + 1))
    fi
  fi
  echo "${i} ${code}" >> "${out_dir}/iterations.txt"
  echo "run ${i}/${count}: exit ${code}"
  i=$((i + 1))
done

# Reads the last value of a numeric JSON field. The top-level totals
# follow the per-test entries in the document, so the last match is the
# overall figure.
json_number() {
  grep -Eo "\"$2\"[[:space:]]*:[[:space:]]*[0-9.eE+-]+" <<< "$1" \
    | tail -n 1 | sed -E 's/.*:[[:space:]]*//'
}

stats_ok=1
infra=""
attempts=""
rate=""
set +e
stats_json="$("$bin" runs stats --json 2>"${out_dir}/stats.err")"
stats_status=$?
set -e
if [ "$stats_status" -eq 0 ]; then
  infra="$(json_number "$stats_json" infraFailures)"
  attempts="$(json_number "$stats_json" attempts)"
  rate="$(json_number "$stats_json" infraFailureRate)"
fi
if [ -z "$infra" ] || [ -z "$attempts" ] || [ -z "$rate" ]; then
  stats_ok=0
fi

over_limit=0
# Reset failures never reach atomyx, and a failed run that left no run
# folder (the device or the runner could not be reached before the run
# began) is invisible to the run statistics. Both are infrastructure
# failures, so they are added to the numerator and the denominator of
# the rate.
if [ "$stats_ok" -eq 1 ]; then
  unseen=$((reset_failures + unrecorded_failures))
  infra=$((infra + unseen))
  attempts=$((attempts + unseen))
  rate="$(awk -v i="$infra" -v a="$attempts" 'BEGIN { printf "%.6f", (a > 0) ? i / a : 0 }')"
  over_limit="$(awk -v r="$rate" -v m="$max_rate" 'BEGIN { print (r + 0 > m + 0) ? 1 : 0 }')"
fi

status=0
if [ "$interrupted" -eq 1 ]; then
  status=130
elif [ "$stats_ok" -eq 0 ] || [ "$over_limit" -eq 1 ]; then
  status=1
fi

{
  echo "platform: ${platform}"
  echo "device: ${device}"
  echo "requested: ${count}"
  echo "total: ${total}"
  echo "passed: ${passed}"
  echo "failed: ${failed}"
  if [ "$stats_ok" -eq 1 ]; then
    echo "infraFailures: ${infra}"
    echo "attempts: ${attempts}"
    echo "infraFailureRate: ${rate}"
  else
    echo "infraFailures: unavailable"
    echo "attempts: unavailable"
    echo "infraFailureRate: unavailable"
  fi
  echo "resetFailures: ${reset_failures}"
  echo "unrecordedFailures: ${unrecorded_failures}"
  echo "maxInfraFailureRate: ${max_rate}"
  if [ "$interrupted" -eq 1 ]; then
    echo "interrupted: yes"
  fi
  echo "exit: ${status}"
} > "${out_dir}/summary.txt"

cat "${out_dir}/summary.txt"
exit "$status"
