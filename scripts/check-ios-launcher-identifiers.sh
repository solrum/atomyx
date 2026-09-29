#!/usr/bin/env bash
#
# Consistency gate: the Go XCUITest launcher hardcodes the Xcode
# project file name, scheme, test target, and runner app bundle name
# it needs to build and run the iOS driver. Those four values are
# not looked up at runtime, so a rename in the iOS project definition
# (project.yml or the Makefile) would silently break the iOS launch
# the next time someone runs it, with no compiler error to catch it.
# This gate derives the same four identifiers from the iOS project
# definition itself and fails loudly the moment they drift apart.
#
# Scope: only the Go launcher (core/internal/iosdriver/xctest_launcher.go)
# and the iOS project definition (platforms/ios-agent/project.yml and
# platforms/ios-agent/Makefile).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

go_file="core/internal/iosdriver/xctest_launcher.go"
project_yml="platforms/ios-agent/project.yml"
makefile="platforms/ios-agent/Makefile"

for f in "$go_file" "$project_yml" "$makefile"; do
  if [ ! -f "$f" ]; then
    echo "ios launcher identifier gate FAILED: expected file not found: $f" >&2
    exit 1
  fi
done

fail=0

# --- Go launcher constants -------------------------------------------------

go_const() {
  # Pull the quoted value out of a `xctestFoo = "..."` const line.
  local name="$1"
  grep -E "^[[:space:]]*${name}[[:space:]]*=" "$go_file" | sed -E 's/.*"([^"]*)".*/\1/'
}

go_project_file_name="$(go_const xctestProjectFileName)"
go_default_scheme="$(go_const xctestDefaultScheme)"
go_runner_app_name="$(go_const xctestRunnerAppName)"
go_only_testing_target="$(go_const xctestOnlyTestingTarget)"

for pair in "xctestProjectFileName:$go_project_file_name" "xctestDefaultScheme:$go_default_scheme" \
            "xctestRunnerAppName:$go_runner_app_name" "xctestOnlyTestingTarget:$go_only_testing_target"; do
  name="${pair%%:*}"
  value="${pair#*:}"
  if [ -z "$value" ]; then
    echo "ios launcher identifier gate FAILED: could not read Go constant $name from $go_file" >&2
    fail=1
  fi
done

# --- iOS project definition -------------------------------------------------

project_name="$(grep -E '^name:' "$project_yml" | head -1 | sed -E 's/^name:[[:space:]]*//')"

# The scheme key is the first mapping key nested one level under the
# top-level `schemes:` section, e.g. `  AtomyxDriver:` under `schemes:`.
project_scheme="$(awk '
  /^schemes:/ { in_schemes = 1; next }
  in_schemes && /^[A-Za-z]/ { in_schemes = 0 }
  in_schemes && /^[[:space:]]+[A-Za-z0-9_]+:[[:space:]]*$/ {
    gsub(/^[[:space:]]+/, "")
    gsub(/:[[:space:]]*$/, "")
    print
    exit
  }
' "$project_yml")"

makefile_project="$(grep -E '^PROJECT[[:space:]]*:=' "$makefile" | head -1 | sed -E 's/^PROJECT[[:space:]]*:=[[:space:]]*//')"
makefile_scheme="$(grep -E '^SCHEME[[:space:]]*:=' "$makefile" | head -1 | sed -E 's/^SCHEME[[:space:]]*:=[[:space:]]*//')"

# The -only-testing target and the Runner app name are read out of the
# `serve:`/`serve-device:` and `install-device:` recipes specifically,
# because the Makefile also has an unrelated -only-testing value for the
# unit-test targets.
makefile_only_testing_target="$(awk '
  /^serve:/ { cap = 1; next }
  /^serve-device:/ { cap = 1; next }
  cap && /^[A-Za-z_-]+:/ { cap = 0 }
  cap && /-only-testing:/ {
    match($0, /-only-testing:[A-Za-z0-9_\/]+/)
    print substr($0, RSTART + length("-only-testing:"), RLENGTH - length("-only-testing:"))
    exit
  }
' "$makefile")"

makefile_runner_app_name="$(awk '
  /^install-device:/ { cap = 1; next }
  cap && /^[A-Za-z_-]+:/ { cap = 0 }
  cap && /-Runner\.app/ {
    match($0, /[A-Za-z0-9_]+-Runner\.app/)
    print substr($0, RSTART, RLENGTH)
    exit
  }
' "$makefile")"

expected_project_file_name="${project_name}.xcodeproj"

report_mismatch() {
  local label="$1" expected="$2" actual="$3"
  echo "ios launcher identifier gate FAILED: $label mismatch" >&2
  echo "  expected (derived from iOS project definition): $expected" >&2
  echo "  actual   (found): $actual" >&2
  fail=1
}

# --- project.yml vs Makefile consistency -----------------------------------

if [ "$expected_project_file_name" != "$makefile_project" ]; then
  report_mismatch "project.yml name vs Makefile PROJECT" "$expected_project_file_name" "$makefile_project"
fi

if [ "$project_scheme" != "$makefile_scheme" ]; then
  report_mismatch "project.yml scheme key vs Makefile SCHEME" "$project_scheme" "$makefile_scheme"
fi

# --- Go launcher constants vs the derived iOS project definition -----------

if [ "$go_project_file_name" != "$makefile_project" ]; then
  report_mismatch "xctestProjectFileName vs Makefile PROJECT" "$makefile_project" "$go_project_file_name"
fi

if [ "$go_default_scheme" != "$makefile_scheme" ]; then
  report_mismatch "xctestDefaultScheme vs Makefile SCHEME" "$makefile_scheme" "$go_default_scheme"
fi

if [ "$go_runner_app_name" != "$makefile_runner_app_name" ]; then
  report_mismatch "xctestRunnerAppName vs Makefile install-device Runner app" "$makefile_runner_app_name" "$go_runner_app_name"
fi

if [ "$go_only_testing_target" != "$makefile_only_testing_target" ]; then
  report_mismatch "xctestOnlyTestingTarget vs Makefile serve -only-testing target" "$makefile_only_testing_target" "$go_only_testing_target"
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi

echo "ios launcher identifier gate passed: Go launcher constants match the iOS project definition."
