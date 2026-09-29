package process

import (
	"bufio"
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecBackground_Start_StreamsStdoutWhileProcessKeepsRunning(t *testing.T) {
	r := NewExecBackground()
	proc, err := r.Start(context.Background(), "sh", "-c", "echo ready; sleep 5")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = proc.Kill() }()

	line, err := bufio.NewReader(proc.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	if line != "ready\n" {
		t.Fatalf("first line = %q, want %q", line, "ready\n")
	}
}

func TestExecBackground_Kill_TerminatesProcessAndUnblocksWait(t *testing.T) {
	r := NewExecBackground()
	proc, err := r.Start(context.Background(), "sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return within timeout after Kill()")
	}
}

func TestExecBackground_Wait_ReportsExitCode(t *testing.T) {
	r := NewExecBackground()
	proc, err := r.Start(context.Background(), "sh", "-c", "exit 7")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	result, waitErr := proc.Wait()
	if waitErr == nil {
		t.Fatal("Wait() error = nil, want a non-zero-exit error")
	}
	if result.ExitCode != 7 {
		t.Fatalf("Wait() ExitCode = %d, want 7", result.ExitCode)
	}
}

func TestExecBackground_Wait_MemoizesResult(t *testing.T) {
	r := NewExecBackground()
	proc, err := r.Start(context.Background(), "sh", "-c", "exit 3")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	first, firstErr := proc.Wait()
	second, secondErr := proc.Wait()
	if first != second {
		t.Fatalf("Wait() results diverged: %+v vs %+v", first, second)
	}
	if (firstErr == nil) != (secondErr == nil) {
		t.Fatalf("Wait() errors diverged: %v vs %v", firstErr, secondErr)
	}
}

func TestExecBackground_Start_AlreadyCanceledContext_ReturnsContextError(t *testing.T) {
	r := NewExecBackground()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Start(ctx, "sh", "-c", "echo hello")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context.Canceled", err)
	}
}

func TestExecBackground_Start_CommandNotFound_ReturnsError(t *testing.T) {
	r := NewExecBackground()
	_, err := r.Start(context.Background(), "definitely-not-a-real-binary-xyz")
	if err == nil {
		t.Fatal("Start() error = nil, want a not-found error")
	}
}

func TestExecBackground_Wait_CapturesStderr(t *testing.T) {
	r := NewExecBackground()
	proc, err := r.Start(context.Background(), "sh", "-c", "echo oops 1>&2; exit 1")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	result, waitErr := proc.Wait()
	if waitErr == nil {
		t.Fatal("Wait() error = nil, want a non-zero-exit error")
	}
	if !strings.Contains(result.Stderr, "oops") {
		t.Fatalf("Wait() Stderr = %q, want it to contain %q", result.Stderr, "oops")
	}
}

// TestExecBackground_Kill_SignalsProcessGroupIncludingGrandchildren
// guards the orphan-grandchild regression: before Setpgid, Kill only
// signaled the direct child, so a background job the child spawned
// (a shell backgrounding a command, for example) survived Kill.
func TestExecBackground_Kill_SignalsProcessGroupIncludingGrandchildren(t *testing.T) {
	r := NewExecBackground()
	proc, err := r.Start(context.Background(), "sh", "-c", "sleep 30 & echo $!; wait")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	line, err := bufio.NewReader(proc.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	grandchildPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse grandchild pid from %q: %v", line, err)
	}

	killDone := make(chan error, 1)
	go func() { killDone <- proc.Kill() }()
	select {
	case err := <-killDone:
		if err != nil {
			t.Fatalf("Kill() error = %v", err)
		}
	case <-time.After(killGracePeriod + 2*time.Second):
		t.Fatal("Kill() did not return")
	}

	if err := syscall.Kill(grandchildPID, 0); err == nil {
		t.Fatalf("grandchild pid %d is still running after Kill, want the whole process group signaled", grandchildPID)
	}
}

// TestExecBackground_Kill_EscalatesToSigkillWhenSigtermIsIgnored
// guards the missing-escalation regression: a process that ignores
// SIGTERM must still be stopped, by Kill escalating to SIGKILL once
// killGracePeriod elapses, instead of leaving the caller blocked or
// the process running forever.
func TestExecBackground_Kill_EscalatesToSigkillWhenSigtermIsIgnored(t *testing.T) {
	r := NewExecBackground()
	proc, err := r.Start(context.Background(), "sh", "-c", `trap "" TERM; while true; do sleep 1; done`)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	killDone := make(chan error, 1)
	go func() { killDone <- proc.Kill() }()
	select {
	case err := <-killDone:
		if err != nil {
			t.Fatalf("Kill() error = %v", err)
		}
	case <-time.After(killGracePeriod + 3*time.Second):
		t.Fatal("Kill() never escalated to SIGKILL for a process that ignores SIGTERM")
	}
}

// TestExecBackground_Start_OutlivesRunFallbackTimeout proves that
// ExecBackground.Start, unlike Exec.Run, applies no fallback timeout
// of its own: a background process started with no context deadline
// keeps running well past the duration Exec's own fallback would have
// killed a synchronous Run call at, because the two runners' spawn
// paths are independent and only Run's is bounded.
func TestExecBackground_Start_OutlivesRunFallbackTimeout(t *testing.T) {
	const (
		standInFallback = 50 * time.Millisecond
		sleepDuration   = 400 * time.Millisecond
	)
	r := NewExecBackground()
	proc, err := r.Start(context.Background(), "sh", "-c", "sleep 0.4")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = proc.Kill() }()

	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Fatalf("background process exited before its %v sleep finished, want it unaffected by a %v fallback bound",
			sleepDuration, standInFallback)
	case <-time.After(standInFallback * 3):
		// Still running well past the stand-in fallback: the
		// background spawn path is not capped, as expected.
	}
}

func TestCappedBuffer_Write_DiscardsBytesBeyondLimit(t *testing.T) {
	w := newCappedBuffer(8)
	if _, err := w.Write([]byte("hello ")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := w.Write([]byte("world")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := w.String(); got != "hello wo" {
		t.Fatalf("String() = %q, want %q", got, "hello wo")
	}
}
