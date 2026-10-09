// Package setup defines the contract for preparing a device to run
// Atomyx: the Step port that one preparation check implements, the
// Status a step reports, and the Result that carries it. Concrete
// steps live in the packages that own the platform tooling they
// drive, and this package stays a leaf that knows nothing about
// devices, adb, or Xcode.
package setup

import "context"

// Status is the outcome of one step.
type Status string

const (
	// StatusOK means the requirement was already satisfied.
	StatusOK Status = "ok"
	// StatusFixed means the step changed the machine or device so the
	// requirement is now satisfied.
	StatusFixed Status = "fixed"
	// StatusManual means the requirement is unmet and only the
	// operator can satisfy it, so the Result carries a Remedy.
	StatusManual Status = "manual"
	// StatusFailed means the step tried and could not establish or
	// verify the requirement.
	StatusFailed Status = "failed"
	// StatusSkipped means the step does not apply to this device or
	// an earlier step already made it moot.
	StatusSkipped Status = "skipped"
)

// Satisfied reports whether the requirement a step guards holds after
// the step ran, which is true for StatusOK and StatusFixed only.
func (s Status) Satisfied() bool {
	return s == StatusOK || s == StatusFixed
}

// Mode selects whether a step may change anything.
type Mode int

const (
	// ModeFix lets a step apply the safe fixes it knows.
	ModeFix Mode = iota
	// ModeCheck makes a step observe only. A step in this mode never
	// mutates the machine or the device.
	ModeCheck
)

// Result is what one step reports. Message states the observed
// outcome. Remedy is the exact action for the operator and is set
// whenever Status is StatusManual or StatusFailed.
type Result struct {
	Status  Status
	Message string
	Remedy  string
}

// Step is one preparation check. Implementations are stateless
// between calls and honour ctx cancellation.
type Step interface {
	// ID is the stable, lower-case identifier reported for the step.
	ID() string
	// Run evaluates the step under mode and reports its Result.
	Run(ctx context.Context, mode Mode) Result
}
