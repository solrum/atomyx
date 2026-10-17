package androiddriver

import (
	"errors"
	"fmt"
	"net/http"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/transport"
)

// agentBusyRemedy is the next step reported when the agent refused a
// request because another action still holds the device. The refused
// request never ran, so repeating it is safe.
const agentBusyRemedy = "Wait for the running gesture to finish, then repeat the call. The refused call did not run."

// agentRejection is implemented by every typed error this package
// returns for an operation the agent refused or could not complete,
// where the agent's own reason is the most useful thing to tell the
// caller. AgentRejection returns the reason to report, which fail
// sanitizes before it crosses the tool boundary, and the next step
// for the caller. A type with no reason to give returns a fixed
// description instead. Adding a typed rejection means implementing
// this interface, not editing fail.
type agentRejection interface {
	error
	AgentRejection() (reason, remedy string)
}

// fail is the single boundary every AndroidDriver method that issues a
// wire call routes its returned error through before it reaches a
// caller. An error implementing agentRejection becomes an
// AGENT_REJECTED carrying its sanitized reason and its remedy, except a
// clear failure caused by missing input focus, which becomes
// INPUT_NOT_FOCUSED so a caller can re-focus and retry. A
// *transport.AgentError with HTTP status 429, the status the
// control server uses for a request it refused because another action
// still holds the device, becomes a retryable AGENT_NOT_READY. Every
// other error goes to diag.ClassifyAgentBoundary, recognizing an
// unreachable agent through transport.IsUnreachable, a caller-input
// rejection through a *transport.AgentError carrying HTTP status 400
// (the status for a request refused because of the arguments it
// carried, such as an unsupported key name), and any other rejection
// through a *transport.AgentError of any other status, so a
// tool-facing consumer never sees the Android agent's HTTP address or
// route path: only Error() (used for logs, never surfaced to a
// caller) carries that detail.
func (d *AndroidDriver) fail(err error) error {
	var rejection agentRejection
	if errors.As(err, &rejection) {
		reason, remedy := rejection.AgentRejection()
		var clearErr *ClearTextError
		if errors.As(err, &clearErr) && clearErr.focusLost() {
			return diag.New(diag.CodeInputNotFocused, diag.SanitizeAgentMessage(reason), remedy, err)
		}
		return diag.New(diag.CodeAgentRejected, diag.SanitizeAgentMessage(reason), remedy, err)
	}
	var agentErr *transport.AgentError
	if errors.As(err, &agentErr) && agentErr.StatusCode == http.StatusTooManyRequests {
		return diag.New(diag.CodeAgentNotReady,
			fmt.Sprintf("the agent on device %s is still running another action", d.opts.Serial),
			agentBusyRemedy,
			err)
	}
	return diag.ClassifyAgentBoundary(err, d.opts.Serial, transport.IsUnreachable,
		func(err error) (string, bool) {
			var agentErr *transport.AgentError
			if errors.As(err, &agentErr) && agentErr.StatusCode == http.StatusBadRequest {
				return agentErr.Message, true
			}
			return "", false
		},
		func(err error) (string, bool) {
			var agentErr *transport.AgentError
			if errors.As(err, &agentErr) {
				return agentErr.Message, true
			}
			return "", false
		})
}
