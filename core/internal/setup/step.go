// Package setup defines the contract for preparing a device to run
// Atomyx: the Step port that one preparation check implements, the
// Registry that orders steps, the runner that executes them in fix or
// check mode, and the Report that carries their outcomes. Concrete
// steps live outside this package, which stays a leaf that knows
// nothing about devices, adb, or Xcode.
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

// Mode selects whether a run may change anything.
type Mode int

const (
	// ModeFix lets the runner apply the fixes that steps offer.
	ModeFix Mode = iota
	// ModeCheck makes the runner observe only. It never calls a
	// step's Fix method, so nothing in this mode mutates the machine
	// or the device.
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
	// Check observes the machine or device and never changes it. It
	// reports StatusOK when the requirement holds, StatusManual with a
	// Remedy when it does not, StatusFailed when it could not tell, and
	// StatusSkipped when the step does not apply.
	Check(ctx context.Context) Result
}

// Fixer is a Step that can also repair an unmet requirement. The
// runner calls Fix only in ModeFix and only after Check reported
// StatusManual, so Fix is the single place a step mutates anything.
type Fixer interface {
	Step
	// Fix repairs the requirement and reports StatusFixed when it now
	// holds. When the repair needs the operator or fails, it reports
	// StatusManual or StatusFailed with a Remedy.
	Fix(ctx context.Context) Result
}
