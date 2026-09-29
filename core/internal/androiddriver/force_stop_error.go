package androiddriver

import "fmt"

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

func (e *ForceStopError) Error() string {
	return fmt.Sprintf("androiddriver: could not stop %s: %s", e.PackageName, e.Reason)
}
