package device

import "fmt"

// LostComponent names which supervised process of a provisioned iOS
// device ended.
type LostComponent string

const (
	// LostRunner is the process that hosts the on-device control plane.
	LostRunner LostComponent = "runner"
	// LostTunnel is the process that maps the host port onto a physical
	// device's runner.
	LostTunnel LostComponent = "tunnel"
)

// ProcessExitError is the cause a session carries when a process it
// depends on ended on its own while the session was open. Cause is the
// classified process outcome and may be nil.
type ProcessExitError struct {
	Component LostComponent
	Cause     error
}

func (e *ProcessExitError) Error() string {
	subject := "the device runner"
	if e.Component == LostTunnel {
		subject = "the device connection tunnel"
	}
	if e.Cause == nil {
		return subject + " exited unexpectedly"
	}
	return fmt.Sprintf("%s exited unexpectedly: %s", subject, e.Cause)
}

func (e *ProcessExitError) Unwrap() error { return e.Cause }
