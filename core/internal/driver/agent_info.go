package driver

// AgentInfo carries what a connected agent reports about its own
// build. A zero field means the value is unknown, either because the
// driver has not connected yet or because the agent does not report it.
type AgentInfo struct {
	// ProtocolVersion is the integer version of the wire contract
	// between the host and the agent. It rises when the contract
	// changes incompatibly, so equality with the host's supported
	// version is the compatibility test.
	ProtocolVersion int

	// AgentVersion is the agent's build version string, for display
	// only. It never decides compatibility.
	AgentVersion string
}
