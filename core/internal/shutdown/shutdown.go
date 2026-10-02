// Package shutdown bounds how long a binary's exit path may run. A
// child process in its own process group no longer receives a
// terminal Ctrl-C, so every binary must stop its children itself, and
// a stuck child must not be able to hold the exit open forever.
package shutdown

import (
	"context"
	"os"
	"syscall"
	"time"

	"dev.atomyx/internal/clock"
)

const (
	// GracePeriod is the most time a binary spends between a stop
	// signal and its exit, cleanup included. Every binary measures its
	// whole exit path against this one value.
	GracePeriod = 10 * time.Second

	// ForcedCleanupBudget is the slice of GracePeriod reserved for a
	// final bounded cleanup attempt when the graceful path has not
	// finished, so the forced exit still releases child processes.
	ForcedCleanupBudget = 2 * time.Second

	// ExitCodeSigint and ExitCodeSigterm follow the shell convention
	// of 128 plus the signal number, so a parent can tell a
	// signal-driven forced exit from an ordinary failure.
	ExitCodeSigint  = 130
	ExitCodeSigterm = 143

	// exitCodeFallback is the forced-exit code for any other signal.
	exitCodeFallback = 1
)

// ExitCodeFor reports the process exit code for a forced exit caused
// by sig.
func ExitCodeFor(sig os.Signal) int {
	switch sig {
	case syscall.SIGINT:
		return ExitCodeSigint
	case syscall.SIGTERM:
		return ExitCodeSigterm
	default:
		return exitCodeFallback
	}
}

// Bounded runs cleanup and reports whether it finished within grace
// as measured by clk. When it did not, cleanup keeps running on its
// own goroutine and the caller is expected to exit the process, which
// reclaims it.
func Bounded(clk clock.Clock, grace time.Duration, cleanup func()) bool {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		cleanup()
	}()
	ctx, stopTimer := context.WithCancel(context.Background())
	defer stopTimer()
	expired := make(chan struct{})
	go func() {
		if clk.Sleep(ctx, grace.Milliseconds()) == nil {
			close(expired)
		}
	}()
	select {
	case <-finished:
		return true
	case <-expired:
		return false
	}
}

// ForceExitAfter starts a watchdog that calls exit once grace has
// elapsed on clk. The returned cancel disarms the watchdog and is
// safe to call more than once. A cancel that completes before the
// grace period lapses guarantees exit is never called.
func ForceExitAfter(clk clock.Clock, grace time.Duration, exit func()) (cancel func()) {
	ctx, disarm := context.WithCancel(context.Background())
	go func() {
		if clk.Sleep(ctx, grace.Milliseconds()) == nil && ctx.Err() == nil {
			exit()
		}
	}()
	return disarm
}

// StopHooks are the steps a binary takes when a stop signal arrives.
// Cancel and Exit are required. CloseInput and Cleanup may be nil.
type StopHooks struct {
	// Cancel cancels the context the binary's main work runs under.
	Cancel func()
	// CloseInput closes a blocking input stream, such as stdin, whose
	// pending read would otherwise keep the main work from observing
	// the cancellation.
	CloseInput func() error
	// Cleanup releases every child process. It may be called while the
	// graceful path is still running it, so it must be idempotent.
	Cleanup func()
	// Exit ends the process with the given code.
	Exit func(code int)
}

// HandleStop runs the stop sequence for sig: it cancels the main
// work, closes the input stream, and arms a watchdog. If the graceful
// path has not ended the process by the time GracePeriod minus
// ForcedCleanupBudget has elapsed, the watchdog runs Cleanup bounded
// by ForcedCleanupBudget and then exits with ExitCodeFor(sig), so the
// whole exit path stays within GracePeriod. The returned disarm
// cancels the watchdog.
func HandleStop(clk clock.Clock, sig os.Signal, hooks StopHooks) (disarm func()) {
	hooks.Cancel()
	if hooks.CloseInput != nil {
		_ = hooks.CloseInput()
	}
	return ForceExitAfter(clk, GracePeriod-ForcedCleanupBudget, func() {
		if hooks.Cleanup != nil {
			Bounded(clk, ForcedCleanupBudget, hooks.Cleanup)
		}
		hooks.Exit(ExitCodeFor(sig))
	})
}
