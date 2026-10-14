package diag

import "fmt"

// NewAgentVersionMismatch builds the CodeAgentVersionMismatch error for
// an agent whose protocol version differs from the one the host
// supports. agentVersion is zero for an agent that reports no protocol
// version at all.
func NewAgentVersionMismatch(agentVersion, hostVersion int) error {
	return New(
		CodeAgentVersionMismatch,
		fmt.Sprintf("the on-device agent speaks protocol version %d, but this host requires version %d", agentVersion, hostVersion),
		"Rebuild and reinstall the agent from the same source tree as this host, then try again.",
		nil,
	)
}
