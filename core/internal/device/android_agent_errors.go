package device

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
)

const (
	adbRemedy       = "Check that the device is connected and unlocked and that adb can reach it, then run atomyx setup again."
	noAdbRemedy     = "Install Android platform-tools and make sure adb is on PATH, then run atomyx setup again."
	noAPKRemedy     = "Build the agent APK or set ATOMYX_ANDROID_APK to a built agent APK, then run atomyx setup again."
	installRemedy   = "Make sure the device is unlocked and allows installs over USB, then run atomyx setup again."
	rebindRemedy    = "Enable Atomyx in Settings, Accessibility on the device, then run atomyx setup again."
	launchRemedy    = "Open the Atomyx app on the device, then run atomyx setup again."
	unreachableText = "the agent could not be reached"
)

// packagePrefix matches the package qualifier that internal errors put
// in front of their text.
var packagePrefix = regexp.MustCompile(`\b(?:agentlauncher|androiddriver|transport|process|device):\s*`)

// adbFailure turns an error from an adb command or from the agent
// launcher into one that carries a diagnostic code, a user-facing
// message without internal package prefixes, and a remedy. Context
// cancellation and errors that already carry a code pass through.
func adbFailure(err error) error {
	if err == nil || isContextError(err) || hasCode(err) {
		return err
	}
	var launchErr *androiddriver.AgentLauncherError
	if errors.As(err, &launchErr) {
		return launcherFailure(launchErr.Code, err)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return diag.New(diag.CodeAgentUnreachable, "adb was not found on PATH", noAdbRemedy, err)
	}
	return diag.New(diag.CodeAgentUnreachable, "adb failed: "+withoutPackagePrefixes(err), adbRemedy, err)
}

// agentUnreachable turns a transport failure into a coded error that
// names neither the address nor the protocol of the agent.
func agentUnreachable(err error) error {
	if err == nil || isContextError(err) || hasCode(err) {
		return err
	}
	return diag.New(diag.CodeAgentUnreachable, unreachableText, launchRemedy, err)
}

func launcherFailure(code androiddriver.AgentLauncherErrorCode, err error) error {
	switch code {
	case androiddriver.ErrCodeNoAdb:
		return diag.New(diag.CodeAgentUnreachable, "adb was not found on PATH", noAdbRemedy, err)
	case androiddriver.ErrCodeNoAPK:
		return diag.New(diag.CodeAppInstallFailed, withoutPackagePrefixes(err), noAPKRemedy, err)
	case androiddriver.ErrCodeInstallFailed:
		return diag.New(diag.CodeAppInstallFailed, withoutPackagePrefixes(err), installRemedy, err)
	case androiddriver.ErrCodeRebindFailed:
		return diag.New(diag.CodeAgentNotReady, withoutPackagePrefixes(err), rebindRemedy, err)
	default:
		return diag.New(diag.CodeAgentNotReady, "the agent did not become ready in time", launchRemedy, err)
	}
}

func withoutPackagePrefixes(err error) string {
	return strings.TrimSpace(packagePrefix.ReplaceAllString(err.Error(), ""))
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func hasCode(err error) bool {
	code, _, _ := diag.Describe(err)
	return code != diag.CodeUnknown
}
