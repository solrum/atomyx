package diag

import (
	"context"
	"errors"
	"fmt"
)

// AgentUnreachableRemedy, InvalidArgumentRemedy, AgentRejectedRemedy,
// and AgentCommunicationFailedRemedy are the operator-facing actions
// ClassifyAgentBoundary attaches to the failure classes it can
// produce, shared across every adapter so a caller sees the same
// guidance regardless of which device kind or method the failure came
// from.
const (
	AgentUnreachableRemedy         = "Confirm the agent is running on the device, then try again."
	InvalidArgumentRemedy          = "Check the arguments against the tool's description, then call it again."
	AgentRejectedRemedy            = "Try the action again. If it keeps failing, check the app's state on the device."
	AgentCommunicationFailedRemedy = "Try the action again. If the problem persists, reconnect to the device."
)

// ClassifyAgentBoundary is the boundary every driver adapter that
// talks to an on-device agent routes a wire-call failure through
// before it reaches a caller. deviceID names the device the failure
// occurred against, folded into the neutral messages this function
// builds. isUnreachable reports whether err represents the agent
// never being reached at all, in whatever shape that adapter's own
// transport uses to signal it. invalidArgument reports whether err
// represents the agent having been reached and rejecting the request
// because of what the caller asked for, such as an unsupported key
// name, returning the agent's own message when so; an adapter with no
// way to tell a caller-input rejection apart from any other kind of
// rejection always returns false. rejected reports whether err
// represents the agent having been reached but rejecting the request
// for any other reason, again returning the agent's own message.
// Both invalidArgument and rejected have their message run through
// SanitizeAgentMessage before it is attached, since a native agent's
// message is not guaranteed to be free of private symbols or wire
// detail.
//
// Classification order:
//
//   - nil stays nil.
//   - a context cancellation or deadline error stays as is, because a
//     caller checks for it with errors.Is rather than through a diag
//     code.
//   - an error that already carries a diag code stays as is, because
//     it was already classified by the layer that produced it.
//   - isUnreachable(err) true becomes CodeAgentUnreachable.
//   - invalidArgument(err) true becomes CodeInvalidArgument, carrying
//     the sanitized agent message.
//   - rejected(err) true becomes CodeAgentRejected, carrying the
//     sanitized agent message.
//   - anything else becomes CodeAgentCommunicationFailed with a
//     neutral message.
//
// err is always preserved as the returned error's Unwrap cause, so a
// caller checking for a specific underlying type still finds it by
// walking the chain with errors.As, and a log line built from the
// returned error can still recover the full wire-level detail.
func ClassifyAgentBoundary(
	err error,
	deviceID string,
	isUnreachable func(error) bool,
	invalidArgument func(error) (message string, ok bool),
	rejected func(error) (message string, ok bool),
) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var coded Coded
	if errors.As(err, &coded) {
		return err
	}
	if isUnreachable(err) {
		return New(CodeAgentUnreachable,
			fmt.Sprintf("could not reach the agent on device %s", deviceID),
			AgentUnreachableRemedy,
			err)
	}
	if message, ok := invalidArgument(err); ok {
		return New(CodeInvalidArgument, SanitizeAgentMessage(message), InvalidArgumentRemedy, err)
	}
	if message, ok := rejected(err); ok {
		return New(CodeAgentRejected, SanitizeAgentMessage(message), AgentRejectedRemedy, err)
	}
	return New(CodeAgentCommunicationFailed,
		fmt.Sprintf("could not complete the request to the agent on device %s", deviceID),
		AgentCommunicationFailedRemedy,
		err)
}
