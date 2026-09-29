package iosdriver

import (
	"errors"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/transport"
)

// fail is the single boundary every IosDriver method that issues a
// wire call over the TCP conn routes its returned error through
// before it reaches a caller. It delegates to diag.ClassifyAgentBoundary,
// recognizing an unreachable agent through transport.IsUnreachable and
// a rejected request through a *transport.AgentError, so a tool-facing
// consumer never sees the runner's TCP address or command name: only
// Error() (used for logs, never surfaced to a caller) carries that
// detail. The runner's ok:false reply carries only an error string, no
// code or category distinguishing a rejection caused by the caller's
// own arguments from any other kind, so this boundary never reports
// diag.CodeInvalidArgument for an iOS failure.
func (d *IosDriver) fail(err error) error {
	return diag.ClassifyAgentBoundary(err, d.opts.UDID, transport.IsUnreachable,
		func(error) (string, bool) { return "", false },
		func(err error) (string, bool) {
			var agentErr *transport.AgentError
			if errors.As(err, &agentErr) {
				return agentErr.Message, true
			}
			return "", false
		})
}
