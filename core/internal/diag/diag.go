// Package diag defines the stable error-code taxonomy every
// user-facing surface (the MCP tool boundary, the CLI) reports a
// failure through. A package anywhere in the module attaches a code
// to a failure by returning an error built with New, or by
// implementing Coded directly on its own error type; Describe then
// recovers that code, wherever it sits in an error's wrapping chain,
// for the boundary that renders it to the caller.
//
// diag imports nothing outside the standard library, so every
// package in the module can depend on it without creating a cycle.
package diag

import "errors"

// Stable error codes. A code is added here only once a real failure
// path attaches it; do not add a code with no current caller.
const (
	// CodeUnknown is Describe's fallback when an error carries no
	// attached code.
	CodeUnknown = "UNKNOWN"
	// CodeDeviceNotFound means a tool or command needs a selected
	// device and none is active.
	CodeDeviceNotFound = "DEVICE_NOT_FOUND"
	// CodeAgentUnreachable means the on-device control surface could
	// not be reached: a connection attempt failed, or an established
	// connection was lost.
	CodeAgentUnreachable = "AGENT_UNREACHABLE"
	// CodeAppInstallFailed means installing an app onto the device
	// failed.
	CodeAppInstallFailed = "APP_INSTALL_FAILED"
	// CodeAppUninstallFailed means removing an app from the device
	// failed.
	CodeAppUninstallFailed = "APP_UNINSTALL_FAILED"
	// CodeAppDataClearFailed means resetting an app's data on the
	// device failed.
	CodeAppDataClearFailed = "APP_DATA_CLEAR_FAILED"
	// CodeAppNotInstalled means a launch was requested for an app id
	// that is not present on the target device.
	CodeAppNotInstalled = "APP_NOT_INSTALLED"
	// CodeToolTimeout means an operation exceeded its time budget and
	// was aborted before it produced a result.
	CodeToolTimeout = "TOOL_TIMEOUT"
	// CodeForegroundUnsupported means the current foreground app was
	// requested on a device where querying it truthfully is not
	// available.
	CodeForegroundUnsupported = "FOREGROUND_UNSUPPORTED"
	// CodeDeviceBusy means a pre-run check found another automation
	// session already driving the target device.
	CodeDeviceBusy = "DEVICE_BUSY"
	// CodeDeviceHeld means another Atomyx process already holds the
	// lease for the target device. It is distinct from CodeDeviceBusy,
	// which is reserved for a foreign, non-Atomyx automation session.
	CodeDeviceHeld = "DEVICE_HELD"
	// CodeDeviceReleasedHeld means a session that gave up its device
	// lease, or lost it, needs the device again and another process now
	// holds the lease.
	CodeDeviceReleasedHeld = "DEVICE_RELEASED_HELD"
	// CodeRunExpired means a tool call targeted a run that was closed
	// because it sat idle past the open-run idle timeout.
	CodeRunExpired = "RUN_EXPIRED"
	// CodeHostPortInUse means the host-side port a device's adb
	// forward needs is already forwarded to a different device, so the
	// forward cannot be created without severing that other device.
	CodeHostPortInUse = "HOST_PORT_IN_USE"
	// CodeInvalidAppID means an app id failed format validation before
	// reaching a device call: it was empty, too long, or contained a
	// character outside the set a package name or bundle id allows.
	CodeInvalidAppID = "INVALID_APP_ID"
	// CodePointerCaptureUnsupported means a pointer gesture path
	// carried a mid-gesture capture marker on a driver that has no way
	// to take a screenshot while the gesture is in flight.
	CodePointerCaptureUnsupported = "POINTER_CAPTURE_UNSUPPORTED"
	// CodeAgentRejected means the on-device agent was reached and
	// answered, but reported that the requested action itself failed,
	// for a reason that is not traceable to a bad caller argument.
	CodeAgentRejected = "AGENT_REJECTED"
	// CodeInvalidArgument means the request itself carries an argument
	// that fails validation: an MCP tool call whose arguments fail
	// schema validation before any wire call is made, or an on-device
	// agent that was reached and reported the request rejected because
	// of what the caller asked for, such as an unsupported key name.
	CodeInvalidArgument = "INVALID_ARGUMENT"
	// CodeAgentCommunicationFailed means a wire call to the on-device
	// agent failed in a way that is neither an unreachable agent nor
	// an explicit rejection the agent reported, such as a malformed
	// response.
	CodeAgentCommunicationFailed = "AGENT_COMMUNICATION_FAILED"
	// CodeAgentNotReady means the on-device agent answered a liveness
	// probe but reported that it cannot currently serve requests, such
	// as an Android accessibility service that is not bound or cannot
	// read the screen. This is distinct from CodeAgentUnreachable,
	// where the agent never answered at all: here the agent is up and
	// the remedy is to fix its readiness, not to relaunch it.
	CodeAgentNotReady = "AGENT_NOT_READY"
	// CodeProxyRequired means a script declares `proxy: required` and
	// the host running it has no network capture adapter configured,
	// so the run was refused before any step executed.
	CodeProxyRequired = "PROXY_REQUIRED"
	// CodeRunnerBuildTimeout means building the on-device runner, or
	// waiting for another Atomyx process that is building it, did not
	// finish within the build timeout. It is distinct from
	// CodeToolTimeout, which bounds a call, and from the device
	// acquisition budget, which excludes the build.
	CodeRunnerBuildTimeout = "RUNNER_BUILD_TIMEOUT"
)

// Coded is implemented by an error that carries a stable code and an
// actionable remedy alongside its message. Describe walks an error's
// wrapping chain looking for the first value that implements it.
// Coded embeds error so a single errors.As call both locates the
// match and gives Describe that match's own Error() text to report,
// rather than a separate unwrap loop to recover it.
type Coded interface {
	error
	// Code reports the stable, machine-readable code identifying the
	// kind of failure.
	Code() string
	// Remedy reports the action a caller can take in response, or an
	// empty string when the failure carries none.
	Remedy() string
}

// taggedError is the error New returns: a fixed code and remedy
// attached to a message, optionally wrapping a cause.
type taggedError struct {
	code    string
	message string
	remedy  string
	cause   error
}

// New builds an error carrying code and remedy, reported to a caller
// as message. cause is the underlying error this failure originated
// from, if any; it is reachable through errors.Unwrap and through
// Describe's own chain walk, but code and remedy always come from
// this returned error rather than from cause.
func New(code, message, remedy string, cause error) error {
	return &taggedError{code: code, message: message, remedy: remedy, cause: cause}
}

func (e *taggedError) Error() string  { return e.message }
func (e *taggedError) Code() string   { return e.code }
func (e *taggedError) Remedy() string { return e.remedy }
func (e *taggedError) Unwrap() error  { return e.cause }

var _ Coded = (*taggedError)(nil)

// Describe recovers the stable code, message, and remedy a user-facing
// surface reports for err. It walks err's wrapping chain looking for
// the first value that implements Coded, using that value's own
// Error() text as the message. When no value in the chain implements
// Coded, Describe returns CodeUnknown, err's own Error() text, and an
// empty remedy. A nil err reports an empty code, message, and remedy.
func Describe(err error) (code, message, remedy string) {
	if err == nil {
		return "", "", ""
	}
	var coded Coded
	if errors.As(err, &coded) {
		return coded.Code(), coded.Error(), coded.Remedy()
	}
	return CodeUnknown, err.Error(), ""
}
