package iosdriver

import (
	"fmt"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/version"
)

// SpeaksHostProtocol reports whether the runner declares exactly the
// wire protocol version this host supports. A runner that reports no
// version counts as version zero, so it never matches.
func (id RunnerIdentity) SpeaksHostProtocol() bool {
	return id.ProtocolVersion == version.AgentProtocolVersion
}

// runnerRebuildCommand rebuilds the runner product from the current
// source tree.
const runnerRebuildCommand = "cd platforms/ios-agent && make build"

// runnerProtocolMismatchError reports a runner this host did not start,
// and so never stops, that speaks a different protocol version. The
// remedy asks the user to stop the runner, because only its owner can.
func runnerProtocolMismatchError(port, runnerVersion int) error {
	_, message, _ := diag.Describe(diag.NewAgentVersionMismatch(runnerVersion, version.AgentProtocolVersion))
	return diag.New(diag.CodeAgentVersionMismatch, message,
		fmt.Sprintf("Stop the runner answering on 127.0.0.1:%d, rebuild it with (%s), and start it again.", port, runnerRebuildCommand),
		nil)
}

// connectProtocolMismatchError reports a connected runner that speaks a
// different protocol version. Connect never stops or relaunches the
// runner, so the remedy covers both the stop and the rebuild.
func connectProtocolMismatchError(runnerVersion int) error {
	_, message, _ := diag.Describe(diag.NewAgentVersionMismatch(runnerVersion, version.AgentProtocolVersion))
	return diag.New(diag.CodeAgentVersionMismatch, message,
		fmt.Sprintf("Stop the runner, rebuild it with (%s), and connect again.", runnerRebuildCommand),
		nil)
}
