#!/usr/bin/env bash
#
# Consistency gate: the host and both native agents each carry the wire
# protocol version as a one-line constant, and the host accepts an agent
# only when the numbers are equal. Three copies of one number drift
# silently, so this gate reads each declaration with an anchored pattern
# that must match exactly once and fails unless all three agree.
#
# Scope: the Go constant in core/internal/version/protocol.go, the
# Android agent constant in
# platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/AgentProtocol.kt,
# and the iOS runner constant in platforms/ios-agent/Tests/AgentProtocol.swift.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

go_file="core/internal/version/protocol.go"
kotlin_file="platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/AgentProtocol.kt"
swift_file="platforms/ios-agent/Tests/AgentProtocol.swift"

go_pattern='^const AgentProtocolVersion = [0-9]+$'
kotlin_pattern='^[[:space:]]+const val AGENT_PROTOCOL_VERSION = [0-9]+$'
swift_pattern='^[[:space:]]+static let version = [0-9]+$'

fail=0

# read_version FILE PATTERN prints the number declared by the single
# line in FILE that matches PATTERN, or reports why it cannot and
# prints nothing.
read_version() {
  local file="$1" pattern="$2" count
  if [ ! -f "$file" ]; then
    echo "agent protocol gate FAILED: expected file not found: $file" >&2
    return 1
  fi
  count="$(grep -Ec "$pattern" "$file" || true)"
  if [ "$count" != "1" ]; then
    echo "agent protocol gate FAILED: $file must declare the version exactly once, found $count matches of: $pattern" >&2
    return 1
  fi
  grep -E "$pattern" "$file" | sed -E 's/.*= ([0-9]+)$/\1/'
}

go_version="$(read_version "$go_file" "$go_pattern")" || fail=1
kotlin_version="$(read_version "$kotlin_file" "$kotlin_pattern")" || fail=1
swift_version="$(read_version "$swift_file" "$swift_pattern")" || fail=1

if [ "$fail" -ne 0 ]; then
  exit 1
fi

if [ "$go_version" != "$kotlin_version" ] || [ "$go_version" != "$swift_version" ]; then
  echo "agent protocol gate FAILED: the three protocol versions differ" >&2
  echo "  host (Go)        $go_version   $go_file" >&2
  echo "  Android agent    $kotlin_version   $kotlin_file" >&2
  echo "  iOS runner       $swift_version   $swift_file" >&2
  echo "  A protocol change edits all three files together." >&2
  exit 1
fi

echo "agent protocol gate passed: host, Android agent, and iOS runner all declare version $go_version."
