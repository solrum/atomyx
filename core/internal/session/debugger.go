package session

import "context"

// Debugger is the holder-side control of a paused run. The host that owns
// the script runner injects one through Request.Debugger. A session
// without one cannot be debugged, so it advertises no debug feature.
//
// Every string a Debugger returns is already redacted by the
// implementer, so a viewer never sees a secret value. Result types are
// owned by this package so that a viewer names no runner type.
//
// Valid only while the session is paused: Resume, Act and Variables.
// SetBreakpoints and Pause are valid at any time while a run is open.
type Debugger interface {
	// SetBreakpoints replaces every breakpoint with locations, each written
	// as "<script file>:<line>" or "inline:<line>". The last writer wins. A
	// location that matches no authored step is an error and leaves the
	// earlier breakpoints in place.
	SetBreakpoints(locations []string) error

	// Pause asks the run to stop before its next step at any depth. It
	// returns at once, and the stop shows as EnterPause on the session.
	Pause() error

	// Resume lets the paused run continue. With opts.Step the run stops
	// again after the next step. Unless opts.Force is set, an advisory
	// check of the next step may refuse to resume, in which case Resumed is
	// false and MissReason says why. A nil error with Resumed false is the
	// only refusal a caller may override with Force.
	//
	// Resume wakes the runner and returns. It must not wait for the runner
	// to reach its next step or to leave the step gate, because the session
	// holds its debug lock for the whole call.
	Resume(ctx context.Context, opts ResumeOptions) (ResumeResult, error)

	// Act runs one step written in script YAML on the device, in a scope
	// that reads the paused variables and discards its writes. A step that
	// nests other steps is refused with an invalid argument error. The
	// session calls it under its debug lock, so it never overlaps a fresh
	// capture or another act.
	Act(ctx context.Context, stepYAML string) (ActResult, error)

	// Variables reports the variables visible at the pause, with secret
	// values masked.
	Variables() (VariablesView, error)
}

// ResumeOptions selects how a paused run continues.
type ResumeOptions struct {
	// Step stops the run again after the next step.
	Step bool
	// Force skips the advisory check of the next step.
	Force bool
}

// ResumeResult reports whether a resume took effect.
type ResumeResult struct {
	// Resumed is false when the advisory check refused to resume.
	Resumed bool `json:"resumed"`
	// MissReason says why the next step looks unable to run. It is advice
	// only and empty when the check found nothing.
	MissReason string `json:"missReason,omitempty"`
}

// ActResult is the outcome of one step run while paused.
type ActResult struct {
	// Command is the step's command name.
	Command string `json:"command"`
	// Outcome is "passed" or "failed".
	Outcome string `json:"outcome"`
	// Message describes a failure and is empty on success.
	Message string `json:"message,omitempty"`
	// DurationMs is how long the step took.
	DurationMs int64 `json:"durationMs"`
	// Artifacts lists the run-relative files the step produced.
	Artifacts []string `json:"artifacts,omitempty"`
}

// VariablesView is the redacted set of variables at a pause.
type VariablesView struct {
	// Variables maps each variable name to its displayed value.
	Variables map[string]string `json:"variables"`
}

// PauseLocation says where a paused run stands.
type PauseLocation struct {
	// Source is the absolute path of the script file, or "inline" for a
	// script passed as text.
	Source string `json:"source"`
	// Line is the step's line, or zero for a nested step that has none.
	Line int `json:"line"`
	// Depth is the nesting depth, zero for a top-level step.
	Depth int `json:"depth"`
	// Command is the command name of the step about to run.
	Command string `json:"command"`
}
