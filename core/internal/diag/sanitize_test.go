package diag

import "testing"

// TestSanitizeAgentMessage covers every leaking shape the audit that
// motivated SanitizeAgentMessage found in native agent error text,
// plus the clean messages that must survive unchanged.
func TestSanitizeAgentMessage(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "unmatched route reported as an HTTP method and path",
			message: "Unknown GET /tree",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "private XCTest symbol in a Swift reflection",
			message: `privateSymbolMissing(symbol: "XCSynthesizedEventRecord")`,
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "Swift error case with a dotted payload",
			message: `tap dispatch failed: SynthesizerError.dispatchFailed(reason: "x")`,
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "connection failure naming an address and port",
			message: "connect to 127.0.0.1:22087 refused",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "dot-qualified Java exception type",
			message: "java.lang.SecurityException: permission denied",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "clean message naming no implementation detail",
			message: "Element not found",
			want:    "Element not found",
		},
		{
			name:    "clean message naming an app id but no route or symbol",
			message: "App dev.atomyx.demo is not installed",
			want:    "App dev.atomyx.demo is not installed",
		},
		{
			name:    "empty message",
			message: "",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "blank message",
			message: "   ",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "bare URL scheme with no method",
			message: "ws://helper/dispatch failed",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "bare route path with no method",
			message: "route /actions/tap unavailable",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "named transport library",
			message: "NanoHTTPD refused the connection",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "stack-trace frame line",
			message: "dispatch failed\n\tat com.atomyx.agent.Dispatcher.send(Dispatcher.kt:88)",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "clock time with a two-digit hour",
			message: "timed out at 12:30",
			want:    "timed out at 12:30",
		},
		{
			name:    "clock time with a one-digit hour",
			message: "took 1:05",
			want:    "took 1:05",
		},
		{
			name:    "hostname followed by a port",
			message: "connect to device.local:22087",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "IPv4 address followed by a port",
			message: "127.0.0.1:8765",
			want:    neutralAgentRejectedMessage,
		},
		{
			name:    "bare port suffix",
			message: "listening on :8765",
			want:    neutralAgentRejectedMessage,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeAgentMessage(tc.message); got != tc.want {
				t.Fatalf("SanitizeAgentMessage(%q) = %q, want %q", tc.message, got, tc.want)
			}
		})
	}
}

// TestSanitizeAgentMessage_CapsOverlongCleanMessage proves that a
// clean message (one matching no leak pattern) that exceeds
// maxAgentMessageLength is truncated rather than surfaced whole or
// replaced with the neutral message.
func TestSanitizeAgentMessage_CapsOverlongCleanMessage(t *testing.T) {
	long := ""
	for len(long) <= maxAgentMessageLength {
		long += "element state invalid, "
	}

	got := SanitizeAgentMessage(long)
	if len(got) != maxAgentMessageLength {
		t.Fatalf("SanitizeAgentMessage(overlong clean message) length = %d, want %d", len(got), maxAgentMessageLength)
	}
	if got != long[:maxAgentMessageLength] {
		t.Fatalf("SanitizeAgentMessage(overlong clean message) = %q, want the first %d bytes of the input", got, maxAgentMessageLength)
	}
}
