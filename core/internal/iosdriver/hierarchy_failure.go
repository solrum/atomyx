package iosdriver

import (
	"errors"
	"strings"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/transport"
)

// snapshotFailedMarker prefixes the runner's rejection of a dumpRawTree
// call when XCTest could not produce an accessibility snapshot. The
// failure is momentary: the app is mid-transition or briefly busy, and
// the next read usually succeeds.
const snapshotFailedMarker = "snapshot failed"

const (
	snapshotNotReadyMessage = "the agent could not read the screen yet"
	snapshotNotReadyRemedy  = "Wait a moment for the screen to settle, then read it again."
)

// failHierarchy routes the error of a hierarchy read through the driver
// boundary, first recognizing a runner rejection that reports a failed
// snapshot and reporting it as AGENT_NOT_READY so a polling caller keeps
// waiting instead of ending on a single bad read.
func (d *IosDriver) failHierarchy(err error) error {
	var agentErr *transport.AgentError
	if errors.As(err, &agentErr) && strings.HasPrefix(agentErr.Message, snapshotFailedMarker) {
		return diag.New(diag.CodeAgentNotReady, snapshotNotReadyMessage, snapshotNotReadyRemedy, err)
	}
	return d.fail(err)
}
