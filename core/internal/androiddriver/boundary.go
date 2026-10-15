package androiddriver

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/transport"
)

// agentBusyRemedy is the next step reported when the agent refused a
// request because another action still holds the device. The refused
// request never ran, so repeating it is safe.
const agentBusyRemedy = "Wait for the running gesture to finish, then repeat the call. The refused call did not run."

const (
	clearTextFailedMessage = "the focused input could not be cleared"
	clearTextFailedRemedy  = "Focus an input field that holds text, then call again."
)

// fail is the single boundary every AndroidDriver method that issues a
// wire call routes its returned error through before it reaches a
// caller. A *transport.AgentError with HTTP status 429, the status the
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
	var clearErr *ClearTextError
	if errors.As(err, &clearErr) {
		message := clearTextFailedMessage
		if strings.TrimSpace(clearErr.Reason) != "" {
			message = diag.SanitizeAgentMessage(clearErr.Reason)
		}
		return diag.New(diag.CodeAgentRejected, message, clearTextFailedRemedy, err)
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
