package netcapture

import (
	"context"
	"fmt"

	"dev.atomyx/internal/runner"
)

// NullCapture is the runner.NetworkCapture adapter used when no proxy
// is configured for a run. It never observes traffic; every capture
// attempt fails with guidance toward configuring an adapter, rather
// than silently reporting an empty capture.
type NullCapture struct{}

var _ runner.NetworkCapture = NullCapture{}

func (NullCapture) Start(context.Context) error { return nil }

func (NullCapture) Stop(context.Context) error { return nil }

func (NullCapture) WaitForRequest(_ context.Context, pattern string, _ int) (runner.CapturedRequest, error) {
	return runner.CapturedRequest{}, fmt.Errorf(
		"capture(%q) requires a network capture adapter but none is configured, "+
			"pass --proxy <type>:<path> to the CLI, or set captureConfig in your script runner options",
		pattern)
}

func (NullCapture) All() []runner.CapturedRequest { return nil }

func (NullCapture) Reset() {}

// IsNoCaptureConfigured always reports true: NullCapture is what a
// composition root wires in place of a real proxy adapter when none
// is configured, so a script declaring `proxy: required` must fail
// the same way it would if no adapter were wired at all.
func (NullCapture) IsNoCaptureConfigured() bool { return true }
