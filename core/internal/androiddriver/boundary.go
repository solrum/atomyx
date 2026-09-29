package androiddriver

import (
	"errors"
	"net/http"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/transport"
)

// fail is the single boundary every AndroidDriver method that issues a
// wire call routes its returned error through before it reaches a
// caller. It delegates to diag.ClassifyAgentBoundary, recognizing an
// unreachable agent through transport.IsUnreachable, a caller-input
// rejection through a *transport.AgentError carrying HTTP status 400
// (the Android control server's status for a request it refused
// because of the arguments it carried, such as an unsupported key
// name), and any other rejection through a *transport.AgentError of
// any other status, so a tool-facing consumer never sees the Android
// agent's HTTP address or route path: only Error() (used for logs,
// never surfaced to a caller) carries that detail.
func (d *AndroidDriver) fail(err error) error {
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
