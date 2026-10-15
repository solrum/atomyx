package androiddriver

import (
	"fmt"
	"strings"
)

// ForceStopError is returned by StopApp, and by LaunchApp's reset
// path, when the device shell command that force-stops the target
// package failed. Reason carries the shell's own output, and the
// app's process and state may have been left untouched.
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
