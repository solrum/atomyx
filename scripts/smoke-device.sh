#!/usr/bin/env bash
# adet smoke test — direct HTTP against device (bypasses MCP layer).
#
# Prerequisites:
#   1. SynapseAgent APK installed and accessibility enabled
#   2. adet foreground service started ("Enable adet" button in app)
#   3. ANDROID_SERIAL env var set, OR exactly one device connected via USB
#
# Usage: bash apps/adet/scripts/smoke-device.sh

set -euo pipefail

PORT="${ADET_PORT:-8765}"
DEVICE_PORT="${ADET_DEVICE_PORT:-8765}"
BASE="http://127.0.0.1:${PORT}"

green() { printf "\033[32m%s\033[0m\n" "$*"; }
red()   { printf "\033[31m%s\033[0m\n" "$*"; }
blue()  { printf "\033[34m%s\033[0m\n" "$*"; }

check_adb() {
  if ! command -v adb >/dev/null 2>&1; then
    red "✗ adb not found on PATH"
    exit 1
  fi
  local count
  count=$(adb devices | tail -n +2 | grep -c '\bdevice$' || true)
  if [[ "$count" -eq 0 ]]; then
    red "✗ no Android device connected"
    exit 1
  fi
  green "✓ adb OK ($count device(s) connected)"
}

setup_forward() {
  blue "→ adb forward tcp:${PORT} → device:${DEVICE_PORT}"
  adb forward "tcp:${PORT}" "tcp:${DEVICE_PORT}" >/dev/null
  green "✓ port forward active"
}

cleanup_forward() {
  adb forward --remove "tcp:${PORT}" >/dev/null 2>&1 || true
}
trap cleanup_forward EXIT

assert_status() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$actual" == "$expected" ]]; then
    green "✓ ${label} → ${actual}"
  else
    red "✗ ${label} expected ${expected}, got ${actual}"
    exit 1
  fi
}

req() {
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -s -o /tmp/adet-resp.json -w "%{http_code}" \
      -X "$method" "${BASE}${path}" \
      -H "content-type: application/json" \
      -d "$body"
  else
    curl -s -o /tmp/adet-resp.json -w "%{http_code}" -X "$method" "${BASE}${path}"
  fi
}

main() {
  check_adb
  setup_forward

  blue "→ GET /health"
  status=$(req GET /health)
  assert_status "/health" "200" "$status"
  cat /tmp/adet-resp.json && echo

  blue "→ GET /tree"
  status=$(req GET /tree)
  assert_status "/tree" "200" "$status"
  el_count=$(grep -o '"elementId"' /tmp/adet-resp.json | wc -l | tr -d ' ')
  green "  elements in tree: ${el_count}"

  blue "→ POST /find {\"text\":\"Settings\"}"
  status=$(req POST /find '{"text":"Settings"}')
  assert_status "/find" "200" "$status"
  cat /tmp/adet-resp.json && echo

  blue "→ GET /current-activity"
  status=$(req GET /current-activity)
  assert_status "/current-activity" "200" "$status"
  cat /tmp/adet-resp.json && echo

  blue "→ GET /screenshot (output: /tmp/adet-screenshot.png)"
  status=$(req GET /screenshot)
  if [[ "$status" == "200" ]]; then
    python3 -c "
import json, base64, sys
d = json.load(open('/tmp/adet-resp.json'))
open('/tmp/adet-screenshot.png','wb').write(base64.b64decode(d['base64']))
print('  saved', len(d['base64']), 'b64 chars')
"
    green "✓ /screenshot OK"
  else
    red "✗ /screenshot returned ${status} (requires API 30+)"
  fi

  blue "→ POST /actions/swipe (visible test: swipe up from center)"
  status=$(req POST /actions/swipe '{"fromX":540,"fromY":1500,"toX":540,"toY":500,"durationMs":300}')
  assert_status "/actions/swipe" "200" "$status"

  green ""
  green "═══════════════════════════════════════"
  green "  All smoke tests passed ✓"
  green "═══════════════════════════════════════"
}

main "$@"
