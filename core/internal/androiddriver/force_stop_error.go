package androiddriver

import (
	"fmt"
	"strings"
)

// ForceStopError is returned by StopApp, and by LaunchApp's reset
// path, when the Android agent could not actually stop the target
// package. A non-privileged agent process holds neither the
// signature-level permission to force-stop another app's process nor
// the permission to kill its background processes, so a refusal here
// means the app's process and state were left untouched, not that the
// request merely failed to reach the device.
type ForceStopError struct {
	PackageName string
	Reason      string
}

const (
	forceStopFailedReason = "the app could not be force-stopped"
	forceStopFailedRemedy = "Confirm the app is installed and the device is connected, then call again. The app was not stopped."
)

// AgentRejection reports the refusal reason, or a fixed description
// when none was given.
func (e *ForceStopError) AgentRejection() (string, string) {
	if strings.TrimSpace(e.Reason) == "" {
		return forceStopFailedReason, forceStopFailedRemedy
	}
	return e.Reason, forceStopFailedRemedy
}

var _ agentRejection = (*ForceStopError)(nil)

func (e *ForceStopError) Error() string {
	return fmt.Sprintf("androiddriver: could not stop %s: %s", e.PackageName, e.Reason)
}
