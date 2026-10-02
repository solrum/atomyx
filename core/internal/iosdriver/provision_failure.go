package iosdriver

import (
	"context"
	"errors"
	"fmt"

	"dev.atomyx/internal/diag"
)

// ProvisionFailure turns an error from bringing the runner up for
// udid into the error a caller reports for an agent it cannot reach: it
// carries diag.CodeAgentUnreachable, the launcher's own message so a
// signing or build problem is named, and a remedy. Cancellation and
// deadline errors, and an error that already carries a code, pass
// through unchanged. The original error stays reachable through
// errors.As.
func ProvisionFailure(udid string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var coded diag.Coded
	if errors.As(err, &coded) {
		return err
	}
	var launchErr *XctestLauncherError
	if errors.As(err, &launchErr) {
		return diag.New(diag.CodeAgentUnreachable, launchErr.Message, launchRemedy(launchErr), err)
	}
	var tunnelErr *IproxyError
	if errors.As(err, &tunnelErr) {
		return diag.New(diag.CodeAgentUnreachable, tunnelErr.Message,
			"Confirm the device is connected and unlocked and that iproxy is installed, then try again.", err)
	}
	return diag.New(diag.CodeAgentUnreachable,
		fmt.Sprintf("could not start the agent on device %s: %v", udid, err),
		"Confirm the device is connected and unlocked, then try again.", err)
}

// launchRemedy names the action that clears a launcher failure, or an
// empty string when its message already says what to do.
func launchRemedy(err *XctestLauncherError) string {
	switch err.Code {
	case XctestErrSimNotBooted:
		return "Boot the simulator, then try again."
	case XctestErrRunnerMismatch:
		return "Stop the other runner or release the port it uses, then try again."
	case XctestErrLaunchTimeout, XctestErrSpawnExit:
		return "Confirm the device is connected and unlocked, then try again."
	default:
		return ""
	}
}
