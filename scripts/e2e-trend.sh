#!/usr/bin/env bash
# Prints the nightly matrix trend, one table per device.
#
# Usage: scripts/e2e-trend.sh [--nights N] [--results-dir DIR]
#
# Reads the last N run folders under the results directory, each laid
# out as <UTC date-time>/<index>-<kind>/result.txt by scripts/nightly.sh.
# Per device (kind and id) it prints one row per night with the e2e
# pass, fail, skip, and unexpected skip counts, the soak passed and
# failed counts, the infrastructure failure rate, and the status. A
# summary line follows each table: green nights out of nights, total
# soak runs, and the pooled infrastructure failure rate, which is the
# sum of infrastructure failures over the sum of soak runs and not an
# average of per-night rates. A value the night did not record prints
# as n/a, and a night where a device is absent adds no row for it.
#
# Environment:
#   ATOMYX_NIGHTLY_RESULTS_DIR  results directory, default e2e-results/nightly
#
# Exit codes:
#   0  the report was printed
#   2  misuse: bad arguments or a missing results directory
set -euo pipefail

readonly DEFAULT_NIGHTS=14

usage() {
  echo "usage: scripts/e2e-trend.sh [--nights N] [--results-dir DIR]" >&2
  echo "  N is a positive integer, default ${DEFAULT_NIGHTS}" >&2
  echo "  DIR defaults to ATOMYX_NIGHTLY_RESULTS_DIR, then e2e-results/nightly" >&2
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
nights="$DEFAULT_NIGHTS"
results_dir="${ATOMYX_NIGHTLY_RESULTS_DIR:-${repo_root}/e2e-results/nightly}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --nights)
      if [ "$#" -lt 2 ]; then
        echo "error: --nights needs a value." >&2
        usage
        exit 2
      fi
      nights="$2"
      shift 2
      ;;
    --results-dir)
      if [ "$#" -lt 2 ]; then
        echo "error: --results-dir needs a value." >&2
        usage
        exit 2
      fi
      results_dir="$2"
      shift 2
      ;;
    -h | --help) usage; exit 0 ;;
    *) echo "error: unknown argument '$1'." >&2; usage; exit 2 ;;
  esac
done

case "$nights" in
  '' | *[!0-9]* | 0)
    echo "error: --nights '${nights}' is not a positive integer." >&2
    exit 2
    ;;
esac
if [ ! -d "$results_dir" ]; then
  echo "error: results directory ${results_dir} does not exist." >&2
  exit 2
fi

# field FILE KEY prints the value of the last "KEY: value" line, or n/a.
field() {
  local v
  v="$(grep -E "^$2: " "$1" 2>/dev/null | tail -1 | sed "s/^$2: //")" || true
  echo "${v:-n/a}"
}

# One tab separated record per device per night, oldest night first.
records="$(
  for night_dir in $(ls -1 "$results_dir" | sort | tail -n "$nights"); do
    [ -d "${results_dir}/${night_dir}" ] || continue
    for entry in "${results_dir}/${night_dir}"/*/; do
      result="${entry}result.txt"
      [ -f "$result" ] || continue
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$(field "$result" kind)" "$(field "$result" id)" "$night_dir" \
        "$(field "$result" "e2e pass")" "$(field "$result" "e2e fail")" \
        "$(field "$result" "e2e skip")" "$(field "$result" "e2e unexpectedSkips")" \
        "$(field "$result" "soak passed")" "$(field "$result" "soak failed")" \
        "$(field "$result" "soak infraFailureRate")" "$(field "$result" status)" \
        "$(field "$result" "soak infraFailures")"
    done
  done
)"

if [ -z "$records" ]; then
  echo "no nightly results found under ${results_dir}"
  exit 0
fi

printf '%s\n' "$records" | awk -F'\t' '
  function num(v) { return v ~ /^[0-9]+$/ }
  {
    key = $1 " " $2
    if (!(key in seen)) { seen[key] = 1; order[++ndev] = key }
    n[key]++
    row[key, n[key]] = $3 "\t" $4 "\t" $5 "\t" $6 "\t" $7 "\t" $8 "\t" $9 "\t" $10 "\t" $11
    if ($11 == "ok") green[key]++
    if (num($8) && num($9)) {
      runs[key] += $8 + $9
      if (num($12)) { infra[key] += $12; pooled_runs[key] += $8 + $9 }
      else if ($10 ~ /^[0-9.]+$/) { infra[key] += $10 * ($8 + $9); pooled_runs[key] += $8 + $9 }
    }
  }
  END {
    for (d = 1; d <= ndev; d++) {
      key = order[d]
      printf "%sdevice %s\n", (d > 1 ? "\n" : ""), key
      printf "%-17s %8s %8s %8s %12s %12s %12s %10s  %s\n", \
        "night", "e2e_pass", "e2e_fail", "e2e_skip", "unexp_skips", "soak_passed", "soak_failed", "infra_rate", "status"
      for (i = 1; i <= n[key]; i++) {
        split(row[key, i], c, "\t")
        printf "%-17s %8s %8s %8s %12s %12s %12s %10s  %s\n", \
          c[1], c[2], c[3], c[4], c[5], c[6], c[7], c[8], c[9]
      }
      pooled = "n/a"
      if (pooled_runs[key] > 0) pooled = sprintf("%.6f", infra[key] / pooled_runs[key])
      printf "summary: green nights %d of %d, soak runs %d, pooled infra failure rate %s\n", \
        green[key], n[key], runs[key], pooled
    }
  }
'
