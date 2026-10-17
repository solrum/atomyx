package diag

import (
	"context"
	"errors"
)

// IsTransient reports whether err describes an agent that may answer
// again shortly. While a foreground activity is recreated or a screen
// is still settling, the agent can drop a connection, answer malformed,
// or report that it cannot read the screen yet, which surface as
// CodeAgentUnreachable, CodeAgentCommunicationFailed, and
// CodeAgentNotReady. Every other error is permanent for a polling wait:
// an invalid argument, a rejected request, a missing device, and an
// uncoded error will not change by polling, and a context error means
// the caller stopped waiting.
func IsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	switch code, _, _ := Describe(err); code {
	case CodeAgentUnreachable, CodeAgentCommunicationFailed, CodeAgentNotReady:
		return true
	}
	return false
}
