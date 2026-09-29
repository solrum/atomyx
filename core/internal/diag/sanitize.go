package diag

import (
	"regexp"
	"strings"
)

// neutralAgentRejectedMessage replaces an agent's rejection message
// when SanitizeAgentMessage judges it unsafe to surface: it names no
// implementation detail, so it stays true regardless of which agent
// or platform produced the message it replaces.
const neutralAgentRejectedMessage = "the agent rejected the request"

// maxAgentMessageLength bounds a sanitized agent message that survives
// the pattern check. An agent message this long is already unusual
// operator-facing text, so truncating it protects a caller rendering
// it in a single line without needing to judge the message's shape
// first.
const maxAgentMessageLength = 200

// agentMessageLeakPatterns are the shapes SanitizeAgentMessage checks
// an agent's rejection message against. Each pattern targets one kind
// of implementation detail a native agent's raw error text can leak.
// Add a new shape as its own entry, with its own comment, rather than
// folding it into an existing pattern, so a future addition stays a
// one-line diff instead of an edit to a shared regular expression.
var agentMessageLeakPatterns = []*regexp.Regexp{
	// A URL or a bare wire scheme, such as "http://" or "ws://".
	regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://`),
	// An IPv4 address, or the loopback hostname.
	regexp.MustCompile(`(?i)\b(?:\d{1,3}\.){3}\d{1,3}\b|\blocalhost\b`),
	// A host-like token (a hostname containing a letter, or an IPv4
	// address) immediately followed by a port, or a bare :port suffix
	// not preceded by a digit, such as ":22087". The digit exclusion on
	// the bare suffix keeps a clock time such as "12:30" or "1:05" from
	// matching, since neither side of a clock time is a valid hostname
	// or port on its own.
	regexp.MustCompile(`(?i)\b[a-z0-9.-]*[a-z][a-z0-9.-]*:\d{2,5}\b|\b(?:\d{1,3}\.){3}\d{1,3}:\d{2,5}\b|(?:^|[^0-9]):\d{2,5}\b`),
	// An HTTP method immediately followed by a route path, such as
	// "GET /tree".
	regexp.MustCompile(`(?i)\b(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+/\S*`),
	// A slash-leading route path, such as "/actions/tap".
	regexp.MustCompile(`(?:^|\s)/[A-Za-z0-9_\-./]+`),
	// A named wire protocol, transport, or server library.
	regexp.MustCompile(`(?i)\b(tcp|http|https|socket|iproxy|NanoHTTPD)\b`),
	// A dot-qualified type name ending in Error or Exception, such as
	// a Java or Kotlin exception class.
	regexp.MustCompile(`\b(?:[A-Za-z_][A-Za-z0-9_]*\.)+[A-Za-z_][A-Za-z0-9_]*(?:Error|Exception)\b`),
	// A bare identifier ending in Error or Exception immediately
	// followed by a member access or a call, such as a Swift error
	// case's dotted payload.
	regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*(?:Error|Exception)\b[.(]`),
	// A private XCTest symbol. Every one of these Apple never
	// documents starts with XC followed by an uppercase letter.
	regexp.MustCompile(`\bXC[A-Z][A-Za-z0-9_]*\b`),
	// A stack-trace frame line, such as "at com.foo.Bar.baz(Bar.kt:12)".
	regexp.MustCompile(`(?m)^\s*at\s+\S+\(.*\)\s*$`),
}

// SanitizeAgentMessage returns message unchanged when it is safe to
// surface to a tool-facing caller, and neutralAgentRejectedMessage
// otherwise. message is judged unsafe when it is empty or blank, or
// when it matches any pattern in agentMessageLeakPatterns: a URL or
// scheme, a host or port, a route path, a named transport, a
// qualified exception type, or a stack-trace frame. A message that
// passes every check is still capped at maxAgentMessageLength, since
// length alone does not make a message unsafe but an overlong one is
// still not fit for a single-line caller surface.
func SanitizeAgentMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return neutralAgentRejectedMessage
	}
	for _, pattern := range agentMessageLeakPatterns {
		if pattern.MatchString(trimmed) {
			return neutralAgentRejectedMessage
		}
	}
	if len(trimmed) > maxAgentMessageLength {
		return trimmed[:maxAgentMessageLength]
	}
	return trimmed
}
