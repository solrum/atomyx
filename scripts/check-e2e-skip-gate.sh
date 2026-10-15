#!/usr/bin/env bash
#
# Behavior test for scripts/e2e-skip-gate.sh and the committed
# scripts/e2e-skip-allowlist.txt. It feeds crafted go test JSON streams
# to the gate and asserts that it passes for allowlisted and
# leg-not-selected skips, fails for an unlisted key, an unkeyed skip, and
# a key allowed only on another leg, and rejects a malformed allowlist.
# It also checks that every key in the committed allowlist is a key
# defined in drivertest/skip.go, and that no driver package calls a raw
# Skip method outside SkipBecause.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
gate="$root/scripts/e2e-skip-gate.sh"
allowlist="$root/scripts/e2e-skip-allowlist.txt"
keys_file="$root/core/internal/driver/drivertest/skip.go"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failures=0

fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

# skip_stream PKG TEST KEY writes the two events go test -json emits for
# a skipped test. An empty KEY writes a skip with no tag.
skip_stream() {
  if [ -n "$3" ]; then
    printf '{"Action":"output","Package":"%s","Test":"%s","Output":"    x_test.go:1: skip[%s]: detail\\n"}\n' "$1" "$2" "$3"
  fi
  printf '{"Action":"output","Package":"%s","Test":"%s","Output":"--- SKIP: %s (0.00s)\\n"}\n' "$1" "$2" "$2"
  printf '{"Action":"skip","Package":"%s","Test":"%s","Elapsed":0}\n' "$1" "$2"
}

# expect NAME WANT_EXIT LEG STREAM_FILE [WANT_TEXT]
expect() {
  local name="$1" want="$2" leg="$3" stream="$4" text="${5:-}" out status
  set +e
  out="$("$gate" "$leg" "$stream" "${ALLOWLIST:-$allowlist}" 2>/dev/null)"
  status=$?
  set -e
  if [ "$status" -ne "$want" ]; then
    fail "$name: exit $status, want $want"
  fi
  if [ -n "$text" ] && ! printf '%s\n' "$out" | grep -qF -- "$text"; then
    fail "$name: output lacks '$text'"
  fi
}

s="$work/pass.json"
{
  skip_stream p TestA/streaming leg-not-selected
  skip_stream p TestB/hier no-hierarchy-staging
  skip_stream p TestC/focus no-focus-input
} > "$s"
expect "allowlisted and not-selected skips pass" 0 ios "$s" "skip-gate: pass"
expect "temporary entries are counted" 0 ios "$s" "skip-allowlisted-temporary: 1"
expect "not-selected skips are counted" 0 ios "$s" "skip-not-selected: 1"

s="$work/unlisted.json"
{
  skip_stream p TestA/ok no-hierarchy-staging
  skip_stream p TestB/probe device-not-found
} > "$s"
expect "unlisted key fails" 1 android "$s" "unexpected-skip: p TestB/probe device-not-found"

s="$work/unkeyed.json"
skip_stream p TestA/raw "" > "$s"
expect "unkeyed skip fails" 1 android "$s" "unexpected-skip: p TestA/raw unkeyed"

s="$work/otherleg.json"
skip_stream p TestA/clear no-clearable-app > "$s"
expect "key allowed only on another leg fails" 1 android "$s" "no-clearable-app"
expect "the same key passes on the leg that lists it" 0 ios "$s"

s="$work/empty.json"
: > "$s"
expect "a run with no skips passes" 0 android "$s" "skip-gate: pass"

expect "unknown leg is misuse" 2 palm "$s"

ALLOWLIST="$work/bad-allowlist.txt"
printf '[permanent]\npalm some-key because\n' > "$ALLOWLIST"
expect "unknown leg in the allowlist is rejected" 2 android "$s"
printf 'android some-key because\n' > "$ALLOWLIST"
expect "entry outside a section is rejected" 2 android "$s"
printf '[permanent]\nandroid some-key\n' > "$ALLOWLIST"
expect "entry without a reason is rejected" 2 android "$s"
unset ALLOWLIST

defined="$(grep -oE 'SkipKey = "[a-z0-9-]+"' "$keys_file" | sed -E 's/.*"([^"]+)"/\1/')"
listed="$(awk '/^[ \t]*#/ || /^[ \t]*$/ || /^\[/ { next } { print $2 }' "$allowlist" | sort -u)"
for key in $listed; do
  if ! printf '%s\n' "$defined" | grep -qx -- "$key"; then
    fail "allowlist key '$key' is not defined in drivertest/skip.go"
  fi
done
if printf '%s\n' "$listed" | grep -qx 'leg-not-selected'; then
  fail "leg-not-selected must not be listed"
fi

raw="$(cd "$root/core" && grep -rnE '\.Skip(f|Now)?\(' \
  internal/driver internal/androiddriver internal/iosdriver internal/iossimdriver \
  --include='*.go' | grep -v 'internal/driver/drivertest/skip.go' || true)"
if [ -n "$raw" ]; then
  fail "raw Skip calls must use drivertest.SkipBecause:
$raw"
fi

if [ "$failures" -ne 0 ]; then
  echo "e2e skip gate check: $failures failure(s)" >&2
  exit 1
fi
echo "e2e skip gate check: all cases passed"
