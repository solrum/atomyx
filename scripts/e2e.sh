#!/usr/bin/env bash
# Runs the device-gated end-to-end suite for one platform and records
# the machine-readable result.
#
# Usage: scripts/e2e.sh <iossim|android|ios>
#
# Exit codes:
#   0  the suite passed and every skip is allowlisted
#   1  a test failed, a skip is not allowlisted, or the runner
#      prerequisite is not met
#   2  misuse: unknown platform or a missing required environment variable
#
# The report lands in e2e-results/<UTC timestamp>-<platform>/ with the
# raw go test JSON stream in go-test.json and the pass, fail, and skip
# counts in summary.txt, followed by the skip gate verdict from
# scripts/e2e-skip-gate.sh and one line per offending skip. Environment
# variable values are never printed, because they can name a specific
# device.
set -euo pipefail

readonly SIM_RUNNER_HOST="127.0.0.1"
readonly SIM_RUNNER_PORT="22087"

usage() {
  echo "usage: scripts/e2e.sh <iossim|android|ios>" >&2
  echo "  iossim   requires ATOMYX_IOS_SIM_UDID and a runner on port ${SIM_RUNNER_PORT}" >&2
  echo "           also requires ATOMYX_IOS_INSTALL_APP (the built demo Runner.app path) and" >&2
  echo "           ATOMYX_IOS_INSTALL_APP_ID (its bundle id, dev.atomyx.demo), which the leg" >&2
  echo "           installs and uses as the launchable, install, and clearable fixture" >&2
  echo "           the path is absolute or relative to the repository root and must be a .app directory" >&2
  echo "           example: examples/atomyx-demo/build/ios-sim/Build/Products/Debug-iphonesimulator/Runner.app" >&2
  echo "           (make demo-ios-sim)" >&2
  echo "  android  requires ATOMYX_ANDROID_SERIAL and the installed dev.atomyx.demo app" >&2
  echo "           optional: ATOMYX_ANDROID_INSTALL_APK and ATOMYX_ANDROID_INSTALL_APP_ID" >&2
  echo "           (APK path and its package name) enable the install roundtrip case" >&2
  echo "           a relative path resolves against the repository root, and a missing file fails the case before the device is touched" >&2
  echo "           the app may already be installed and is restored after the case" >&2
  echo "           example: examples/atomyx-demo/build/app/outputs/flutter-apk/app-debug.apk" >&2
  echo "           with app id dev.atomyx.demo (flutter build apk --debug in examples/atomyx-demo)" >&2
  echo "  ios      requires ATOMYX_IOS_DEVICE_UDID, a runner served for the device, and the signed demo app:" >&2
  echo "           ATOMYX_IOS_INSTALL_APP (the signed Release-iphoneos Runner.app path) and" >&2
  echo "           ATOMYX_IOS_INSTALL_APP_ID (its bundle id, dev.atomyx.demo), which the leg" >&2
  echo "           installs and uses as the launchable, install, and clearable fixture" >&2
  echo "           the path is absolute or relative to the repository root and must be a .app directory" >&2
  echo "           example: examples/atomyx-demo/build/ios-device/Build/Products/Release-iphoneos/Runner.app" >&2
  echo "           (make demo-ios-device)" >&2
}

platform="${1:-}"

case "$platform" in
  iossim)
    required_var="ATOMYX_IOS_SIM_UDID"
    packages=(./internal/iosdriver/...)
    ;;
  android)
    required_var="ATOMYX_ANDROID_SERIAL"
    packages=(./internal/androiddriver/...)
    ;;
  ios)
    required_var="ATOMYX_IOS_DEVICE_UDID"
    packages=(./internal/iosdriver/...)
    ;;
  *)
    usage
    exit 2
    ;;
esac

if [ -z "${!required_var:-}" ]; then
  echo "error: ${required_var} is not set for platform ${platform}." >&2
  echo "Set it to the target device identifier and run again." >&2
  usage
  exit 2
fi

if [ "$platform" = "iossim" ] || [ "$platform" = "ios" ]; then
  for demo_var in ATOMYX_IOS_INSTALL_APP ATOMYX_IOS_INSTALL_APP_ID; do
    if [ -z "${!demo_var:-}" ]; then
      echo "error: ${demo_var} is not set for platform ${platform}." >&2
      usage
      exit 2
    fi
  done
fi

if [ "$platform" = "iossim" ]; then
  if ! nc -z "$SIM_RUNNER_HOST" "$SIM_RUNNER_PORT" >/dev/null 2>&1; then
    echo "error: no iOS runner answers on ${SIM_RUNNER_HOST}:${SIM_RUNNER_PORT}." >&2
    echo "Start it with 'make serve' in platforms/ios-agent against the booted simulator, then run again." >&2
    exit 1
  fi
fi

# The simulator driver tests need the atomyx-sim-hid helper, which is not
# built by default. That leg runs only when ATOMYX_SIM_HID_BIN names an
# executable file, and otherwise the skip is recorded in the summary.
sim_hid_skip_note=""
if [ "$platform" = "iossim" ]; then
  if [ -n "${ATOMYX_SIM_HID_BIN:-}" ] && [ -f "$ATOMYX_SIM_HID_BIN" ] && [ -x "$ATOMYX_SIM_HID_BIN" ]; then
    packages=(./internal/iossimdriver/... "${packages[@]}")
  else
    sim_hid_skip_note="simulator HID helper leg skipped: set ATOMYX_SIM_HID_BIN to an executable atomyx-sim-hid binary to run the iossimdriver package"
  fi
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
out_dir="${repo_root}/e2e-results/${stamp}-${platform}"
mkdir -p "$out_dir"

export ATOMYX_DRIVER_E2E="$platform"

echo "Running the ${platform} end-to-end suite, report in ${out_dir}"
if [ -n "$sim_hid_skip_note" ]; then
  echo "$sim_hid_skip_note"
fi

set +e
(cd "${repo_root}/core" && go test -tags e2e -count=1 -json "${packages[@]}") | tee "${out_dir}/go-test.json" >/dev/null
test_status="${PIPESTATUS[0]}"
set -e

# Count only test-level events, which carry a "Test" field, so package
# summary lines do not inflate the totals.
count_action() {
  local n
  n="$(grep -c "\"Action\":\"$1\",\"Package\":\"[^\"]*\",\"Test\":" "${out_dir}/go-test.json" || true)"
  echo "${n:-0}"
}

pass_count="$(count_action pass)"
fail_count="$(count_action fail)"
skip_count="$(count_action skip)"

# Every skip must carry a reason key that the allowlist names for this
# platform. An unlisted or untagged skip fails an otherwise green leg.
set +e
gate_output="$("${repo_root}/scripts/e2e-skip-gate.sh" "$platform" "${out_dir}/go-test.json" "${repo_root}/scripts/e2e-skip-allowlist.txt")"
gate_status="$?"
set -e
if [ "$gate_status" -ne 0 ] && [ "$test_status" -eq 0 ]; then
  test_status=1
fi

{
  echo "platform: ${platform}"
  echo "pass: ${pass_count}"
  echo "fail: ${fail_count}"
  echo "skip: ${skip_count}"
  echo "$gate_output"
  echo "exit: ${test_status}"
  if [ -n "$sim_hid_skip_note" ]; then
    echo "$sim_hid_skip_note"
  fi
} > "${out_dir}/summary.txt"

cat "${out_dir}/summary.txt"
exit "$test_status"
