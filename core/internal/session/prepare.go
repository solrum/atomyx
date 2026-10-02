package session

import (
	"context"
	"errors"
	"fmt"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
)

// errBuildBudget is the cancellation cause of a Prepare whose build
// timeout elapsed, so it can be told apart from a canceled caller.
var errBuildBudget = errors.New("runner build timeout elapsed")

// Prepare does the slow work a device needs before Open can take it,
// such as compiling the iOS runner, without holding the device lease.
// Run it before Open so a long build neither blocks other processes
// from the device nor spends Open's acquisition budget.
//
// It is bounded by buildMs on deps.Clock, which covers the wait for
// another process's build of the same runner. A non-positive buildMs
// means DefaultRunnerBuildTimeoutMs. When the time elapses it fails with
// RUNNER_BUILD_TIMEOUT. A canceled ctx returns its own error. With no
// Preparer in deps it does nothing.
func Prepare(ctx context.Context, deps Deps, desc device.Descriptor, buildMs int64) error {
	if deps.Preparer == nil {
		return nil
	}
	if buildMs <= 0 {
		buildMs = DefaultRunnerBuildTimeoutMs
	}
	deps = deps.withDefaults()
	budget, stop := withClockTimeout(ctx, deps.Clock, buildMs, errBuildBudget)
	err := deps.Preparer.Prepare(budget, desc)
	elapsed := errors.Is(context.Cause(budget), errBuildBudget)
	stop()
	if err == nil || ctx.Err() != nil || !elapsed {
		return err
	}
	return diag.New(diag.CodeRunnerBuildTimeout,
		fmt.Sprintf("building the device runner did not finish within %s", formatBudget(buildMs)),
		"Wait for any other Atomyx process that is building the runner to finish, check that Xcode is installed and responsive, then try again.", err)
}

// withClockTimeout returns a context derived from parent that ends with
// cause as its cause once ms milliseconds of clk time pass.
// The returned stop function releases the timer and must be called.
func withClockTimeout(parent context.Context, clk clock.Clock, ms int64, cause error) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if clk.Sleep(ctx, ms) == nil {
			cancel(cause)
		}
	}()
	return ctx, func() {
		cancel(context.Canceled)
		<-done
	}
}
