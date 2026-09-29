package process

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
)

// fallbackTestBudget is the outer bound the fallback-timeout tests
// hold Run to: generous enough that scheduling noise never trips it,
// far short of the hung command's own sleep duration, so a
// regression that stops the fallback from firing fails the test
// instead of hanging it.
const fallbackTestBudget = 2 * time.Second

func TestExec_Run_CapturesStdoutAndSucceeds(t *testing.T) {
	r := NewExec()
	result, err := r.Run(context.Background(), "sh", "-c", "echo hello")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Stdout != "hello\n" {
		t.Fatalf("Run() Stdout = %q, want %q", result.Stdout, "hello\n")
	}
	if result.ExitCode != 0 {
		t.Fatalf("Run() ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestExec_Run_NonZeroExit_ReturnsErrorAndCapturesStderr(t *testing.T) {
	r := NewExec()
	result, err := r.Run(context.Background(), "sh", "-c", "echo boom >&2; exit 3")
	if err == nil {
		t.Fatal("Run() error = nil, want a non-zero-exit error")
	}
	if result.ExitCode != 3 {
		t.Fatalf("Run() ExitCode = %d, want 3", result.ExitCode)
	}
	if result.Stderr != "boom\n" {
		t.Fatalf("Run() Stderr = %q, want %q", result.Stderr, "boom\n")
	}
}

func TestExec_Run_CommandNotFound_ReturnsError(t *testing.T) {
	r := NewExec()
	_, err := r.Run(context.Background(), "definitely-not-a-real-binary-xyz")
	if err == nil {
		t.Fatal("Run() error = nil, want a not-found error")
	}
}

func TestExec_Run_AlreadyCanceledContext_ReturnsContextError(t *testing.T) {
	r := NewExec()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Run(ctx, "sh", "-c", "echo hello")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestExec_Run_ContextCanceledMidFlight_AbortsAndReturnsContextError(t *testing.T) {
	r := NewExec()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := r.Run(ctx, "sh", "-c", "sleep 5")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context.DeadlineExceeded", err)
	}
}

// TestExec_Run_FallbackTimeout_ClassifiesTimeoutAndKillsProcess proves
// that, against a ctx with no deadline of its own, a command that
// never finishes is killed and reaped once the runner's own fallback
// timeout elapses, and that the failure is reported as
// diag.CodeToolTimeout rather than the raw kill error. The command
// execs into sleep so the pid it prints before hanging stays the pid
// of the process Run ultimately kills.
func TestExec_Run_FallbackTimeout_ClassifiesTimeoutAndKillsProcess(t *testing.T) {
	const shortFallback = 50 * time.Millisecond
	r := NewExec(WithRunFallbackTimeout(shortFallback))

	start := time.Now()
	result, err := r.Run(context.Background(), "sh", "-c", "echo $$; exec sleep 30")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run() against a hung command returned no error")
	}
	if elapsed > fallbackTestBudget {
		t.Fatalf("Run() took %v against a %v fallback, want it bounded by %v rather than waiting out the full sleep",
			elapsed, shortFallback, fallbackTestBudget)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
		t.Fatalf("Run() against a hung command: diag.Describe code = %q, want %q (error: %v)", code, diag.CodeToolTimeout, err)
	}

	pid, parseErr := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if parseErr != nil {
		t.Fatalf("parse pid from %q: %v", result.Stdout, parseErr)
	}
	waitUntil(t, "hung process reaped after fallback timeout", func() bool {
		return syscall.Kill(pid, 0) != nil
	})
}

// TestExec_Run_CallerDeadlineNotShortened_NoTimeoutFallback proves
// that Run never applies its fallback timeout when ctx already
// carries its own deadline, even one longer than the fallback: the
// command keeps running until the caller's own deadline fires,
// reported as ctx.Err() directly rather than reclassified as
// diag.CodeToolTimeout.
func TestExec_Run_CallerDeadlineNotShortened_NoTimeoutFallback(t *testing.T) {
	const (
		shortFallback    = 10 * time.Millisecond
		callerDeadline   = 300 * time.Millisecond
		minObservedDelay = 250 * time.Millisecond
	)
	r := NewExec(WithRunFallbackTimeout(shortFallback))
	ctx, cancel := context.WithTimeout(context.Background(), callerDeadline)
	defer cancel()

	start := time.Now()
	_, err := r.Run(ctx, "sh", "-c", "sleep 5")
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context.DeadlineExceeded from the caller's own deadline", err)
	}
	if elapsed < minObservedDelay {
		t.Fatalf("Run() returned after %v, want it bounded by the caller's own %v deadline rather than the shorter fallback",
			elapsed, callerDeadline)
	}
	if code, _, _ := diag.Describe(err); code == diag.CodeToolTimeout {
		t.Fatalf("Run() against a caller deadline: diag.Describe code = %q, want the raw context error, not the fallback's classification", code)
	}
}
