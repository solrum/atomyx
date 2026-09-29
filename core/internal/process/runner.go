package process

import "context"

// Result is the captured outcome of one external command invocation:
// its standard output, standard error, and process exit status.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner executes an external command and captures its output. An
// adapter that needs to shell out (adb, xcrun, and similar
// device-tooling CLIs) takes a Runner by constructor injection
// instead of calling os/exec directly, so a test can substitute a
// fake and assert the exact command it dispatched without a real
// process or device.
//
// Run returns a non-nil error when the command cannot be started, ctx
// is canceled or expires before the command finishes, or the command
// exits with a non-zero status. Result is still populated with
// whatever output was captured before that failure, so a caller that
// needs the partial output alongside a non-zero exit inspects Result
// even when err is non-nil.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}
