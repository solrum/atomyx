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

echo "Soak: ${count} runs on ${platform}, report in ${out_dir}"

i=1
while [ "$i" -le "$count" ] && [ "$interrupted" -eq 0 ]; do
  log="${out_dir}/iter-$(printf '%04d' "$i").log"
  set +e
  "$bin" run --file "$flow" --platform "$cli_platform" --device "$device" > "$log" 2>&1
  code=$?
  set -e
  total=$((total + 1))
  if [ "$code" -eq 0 ]; then
    passed=$((passed + 1))
  else
    failed=$((failed + 1))
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
if [ "$stats_ok" -eq 1 ]; then
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
  echo "maxInfraFailureRate: ${max_rate}"
  if [ "$interrupted" -eq 1 ]; then
    echo "interrupted: yes"
  fi
  echo "exit: ${status}"
} > "${out_dir}/summary.txt"

cat "${out_dir}/summary.txt"
exit "$status"
