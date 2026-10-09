// Package shutdown bounds how long a binary's exit path may run. A
// child process in its own process group no longer receives a
// terminal Ctrl-C, so every binary must stop its children itself, and
// a stuck child must not be able to hold the exit open forever.
package shutdown

import (
	"context"
	"errors"
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

	// ParentGracePeriod is the exit budget of a process that stops child
	// processes of its own, each of which gets GracePeriod to exit and then
	// a bounded kill. It is GracePeriod plus the kill escalation, the time
	// to write the parent's own results, and a safety margin, so the
	// watchdog of a parent never fires before its children are gone.
	ParentGracePeriod = GracePeriod + 8*time.Second

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

// interrupt is the cancellation cause recorded when a stop signal
// cancels a context, so a consumer can tell a user interrupt apart from
// any other context.Canceled.
type interrupt struct{ sig os.Signal }

func (i interrupt) Error() string { return "interrupted by " + i.sig.String() }

// InterruptCause returns the cancellation cause to pass to a
// context.CancelCauseFunc when sig stops the process.
func InterruptCause(sig os.Signal) error { return interrupt{sig: sig} }

// InterruptExitCode reports the exit code for a context that a stop
// signal canceled, using the same mapping as ExitCodeFor. It reports
// false when ctx is live or was canceled for any other reason.
func InterruptExitCode(ctx context.Context) (int, bool) {
	var i interrupt
	if !errors.As(context.Cause(ctx), &i) {
		return 0, false
	}
	return ExitCodeFor(i.sig), true
}

// Bounded runs cleanup and reports whether it finished within grace
// as measured by clk. When it did not, cleanup keeps running on its
// own goroutine and the caller is expected to exit the process, which
// reclaims it.
//
// The grace timer inherits only the values of ctx, never its
// cancellation, so cleanup gets its full grace period even when the
// caller's context is already done.
func Bounded(ctx context.Context, clk clock.Clock, grace time.Duration, cleanup func()) bool {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		cleanup()
	}()
	timerCtx, stopTimer := context.WithCancel(context.WithoutCancel(ctx))
	defer stopTimer()
	expired := make(chan struct{})
	go func() {
		if clk.Sleep(timerCtx, grace.Milliseconds()) == nil {
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
	// Grace is the whole exit budget of the process. Zero means
	// GracePeriod, and a process that stops children of its own sets
	// ParentGracePeriod.
	Grace time.Duration
	// Cancel cancels the context the binary's main work runs under.
	Cancel func()
	// CloseInput closes a blocking input stream, such as stdin, whose
	// pending read would otherwise keep the main work from observing
	// the cancellation.
	CloseInput func() error
	// Cleanup releases every child process. It may be called while the
	// graceful path is still running it, so it must be idempotent. When
	// the watchdog bounds it by ForcedCleanupBudget and it has not
	// finished, the process exits anyway, so a child it was still
	// releasing, such as an iOS runner, may outlive the process. The next
	// holder of that device adopts or reaps it.
	Cleanup func()
	// Exit ends the process with the given code.
	Exit func(code int)
}

// HandleStop runs the stop sequence for sig: it cancels the main
// work, closes the input stream, and arms a watchdog. If the graceful
// path has not ended the process by the time GracePeriod minus
// ForcedCleanupBudget has elapsed, the watchdog runs Cleanup bounded
// by ForcedCleanupBudget and then exits with ExitCodeFor(sig), so the
// whole exit path stays within the budget, GracePeriod unless hooks.Grace
// says otherwise. The returned disarm
// cancels the watchdog.
func HandleStop(clk clock.Clock, sig os.Signal, hooks StopHooks) (disarm func()) {
	hooks.Cancel()
	if hooks.CloseInput != nil {
		_ = hooks.CloseInput()
	}
	grace := hooks.Grace
	if grace <= 0 {
		grace = GracePeriod
	}
	return ForceExitAfter(clk, grace-ForcedCleanupBudget, func() {
		if hooks.Cleanup != nil {
			Bounded(context.Background(), clk, ForcedCleanupBudget, hooks.Cleanup)
		}
		hooks.Exit(ExitCodeFor(sig))
	})
}
