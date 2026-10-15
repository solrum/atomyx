#!/usr/bin/env bash
# Judges the skips in one device e2e run against the committed allowlist.
#
# Usage: scripts/e2e-skip-gate.sh <leg> <go-test.json> <allowlist>
#
# leg is android, ios, or iossim. go-test.json is the go test -json
# stream of the run. Every skipped test must carry a skip[key] tag
# written by drivertest.SkipBecause, and its key must be listed for the
# leg in the allowlist, except leg-not-selected, which is counted and
# never judged.
#
# Output on stdout, one line each, suitable for summary.txt:
#   skip-gate: pass|fail
#   skip-not-selected: <n>
#   skip-allowlisted-permanent: <n>
#   skip-allowlisted-temporary: <n>
#   unexpected-skip: <package> <test> <key>        (one per offender)
#
# Exit codes: 0 gate passed, 1 gate failed, 2 misuse or unreadable input.
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: scripts/e2e-skip-gate.sh <android|ios|iossim> <go-test.json> <allowlist>" >&2
  exit 2
fi

leg="$1"
results="$2"
allowlist="$3"

case "$leg" in
  android|ios|iossim) ;;
  *) echo "error: unknown leg: ${leg}" >&2; exit 2 ;;
esac
for f in "$results" "$allowlist"; do
  if [ ! -r "$f" ]; then
    echo "error: cannot read ${f}" >&2
    exit 2
  fi
done

# Entries become "A <leg> <key> <section>" lines. Unknown legs and
# unknown sections are an error so a typo cannot silently widen the gate.
entries="$(awk '
  /^[ \t]*#/ || /^[ \t]*$/ { next }
  /^\[permanent\][ \t]*$/ { section = "permanent"; next }
  /^\[temporary\][ \t]*$/ { section = "temporary"; next }
  {
    if (section == "") { print "E no section before: " $0; next }
    if ($1 != "android" && $1 != "ios" && $1 != "iossim") { print "E unknown leg: " $0; next }
    if ($2 == "" || NF < 3) { print "E entry needs a key and a reason: " $0; next }
    print "A " $1 " " $2 " " section
  }
' "$allowlist")"

if printf '%s\n' "$entries" | grep -q '^E '; then
  printf '%s\n' "$entries" | grep '^E ' | sed 's/^E /error: allowlist: /' >&2
  exit 2
fi

# S lines are skip events, K lines are the tagged output of a test.
tab="$(printf '\t')"
events="$(sed -nE \
  -e 's/.*"Action":"skip","Package":"([^"]*)","Test":"([^"]*)".*/S'"$tab"'\1'"$tab"'\2/p' \
  -e 's/.*"Action":"output","Package":"([^"]*)","Test":"([^"]*)","Output":".*skip\[([a-z0-9-]+)\].*/K'"$tab"'\1'"$tab"'\2'"$tab"'\3/p' \
  "$results")"

verdict="$(
  { printf '%s\n' "$entries"; printf '%s\n' "$events"; } | awk -F'\t' -v leg="$leg" '
    /^A / { split($0, a, " "); if (a[2] == leg) allowed[a[3]] = a[4]; next }
    $1 == "K" { key[$2 SUBSEP $3] = $4; next }
    $1 == "S" { n++; pkg[n] = $2; test[n] = $3; next }
    END {
      for (i = 1; i <= n; i++) {
        k = key[pkg[i] SUBSEP test[i]]
        if (k == "") k = "unkeyed"
        if (k == "leg-not-selected") notsel++
        else if (allowed[k] == "permanent") perm++
        else if (allowed[k] == "temporary") temp++
        else { bad++; offenders = offenders "unexpected-skip: " pkg[i] " " test[i] " " k "\n" }
      }
      printf "skip-gate: %s\n", (bad > 0 ? "fail" : "pass")
      printf "skip-not-selected: %d\n", notsel
      printf "skip-allowlisted-permanent: %d\n", perm
      printf "skip-allowlisted-temporary: %d\n", temp
      printf "%s", offenders
    }'
)"

printf '%s\n' "$verdict"
case "$verdict" in
  "skip-gate: pass"*) exit 0 ;;
  *) exit 1 ;;
esac
