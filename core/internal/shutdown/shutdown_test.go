package shutdown

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
)

const (
	// settleTime is how long a test waits to be sure an exit did not happen.
	settleTime = 100 * time.Millisecond

	testGraceMs = int64(30)
	testTimeout = 10 * time.Second
)

func TestBoundedReportsCompletion(t *testing.T) {
	ran := false
	if !Bounded(context.Background(), clock.SystemClock{}, time.Second, func() { ran = true }) {
		t.Fatal("Bounded returned false for a cleanup that finished")
	}
	if !ran {
		t.Fatal("cleanup did not run")
	}
}

func TestBoundedGivesUpOnAHungCleanup(t *testing.T) {
	clk := clock.NewManualClock(0)
	release := make(chan struct{})
	defer close(release)
	result := make(chan bool, 1)
	go func() { result <- Bounded(context.Background(), clk, time.Second, func() { <-release }) }()
	clk.AwaitSleepers(1)
	clk.Advance(time.Second.Milliseconds())
	select {
	case finished := <-result:
		if finished {
			t.Fatal("Bounded returned true for a cleanup that never finished")
		}
	case <-time.After(testTimeout):
		t.Fatal("Bounded did not give up after the grace period")
	}
}

func TestBoundedKeepsTheGraceTimerWhenTheContextIsCanceled(t *testing.T) {
	clk := clock.NewManualClock(0)
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := make(chan bool, 1)
	go func() { result <- Bounded(ctx, clk, time.Second, func() { <-release }) }()
	clk.AwaitSleepers(1)
	select {
	case <-result:
		t.Fatal("Bounded gave up before the grace period elapsed")
	case <-time.After(time.Duration(testGraceMs) * time.Millisecond):
	}
	clk.Advance(time.Second.Milliseconds())
	select {
	case finished := <-result:
		if finished {
			t.Fatal("Bounded returned true for a cleanup that never finished")
		}
	case <-time.After(testTimeout):
		t.Fatal("Bounded did not give up after the grace period")
	}
}

func TestForceExitAfterFiresWhenGraceElapses(t *testing.T) {
	clk := clock.NewManualClock(0)
	fired := make(chan struct{})
	ForceExitAfter(clk, time.Duration(testGraceMs)*time.Millisecond, func() { close(fired) })
	clk.AwaitSleepers(1)
	clk.Advance(testGraceMs)
	select {
	case <-fired:
	case <-time.After(testTimeout):
		t.Fatal("watchdog never fired")
	}
}

func TestForceExitAfterCanBeDisarmedBeforeTheGraceElapses(t *testing.T) {
	clk := clock.NewManualClock(0)
	var fired atomic.Bool
	cancel := ForceExitAfter(clk, time.Duration(testGraceMs)*time.Millisecond, func() { fired.Store(true) })
	clk.AwaitSleepers(1)
	cancel()
	cancel()
	// A canceled Sleep leaves the clock with an error, which the
	// watchdog treats as disarmed, so an empty sleeper list means the
	// decision not to fire has been made.
	clk.AwaitIdle()
	clk.Advance(testGraceMs)
	if fired.Load() {
		t.Fatal("watchdog fired after cancel")
	}
}

func TestExitCodeForMapsSignals(t *testing.T) {
	cases := []struct {
		sig  os.Signal
		want int
	}{
		{syscall.SIGINT, ExitCodeSigint},
		{syscall.SIGTERM, ExitCodeSigterm},
		{syscall.SIGHUP, exitCodeFallback},
	}
	for _, c := range cases {
		if got := ExitCodeFor(c.sig); got != c.want {
			t.Errorf("ExitCodeFor(%v) = %d, want %d", c.sig, got, c.want)
		}
	}
}

type stopRecorder struct {
	mu     sync.Mutex
	events []string
	exited chan int
}

func newStopRecorder() *stopRecorder { return &stopRecorder{exited: make(chan int, 1)} }

func (r *stopRecorder) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *stopRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *stopRecorder) hooks(cleanup func()) StopHooks {
	return StopHooks{
		Cancel:     func() { r.record("cancel") },
		CloseInput: func() error { r.record("close-input"); return nil },
		Cleanup:    cleanup,
		Exit:       func(code int) { r.record("exit"); r.exited <- code },
	}
}

func waitExit(t *testing.T, r *stopRecorder) int {
	t.Helper()
	select {
	case code := <-r.exited:
		return code
	case <-time.After(testTimeout):
		t.Fatal("exit was never called")
		return 0
	}
}

func TestHandleStopCleansUpBeforeExitingWhenTheGracefulPathHangs(t *testing.T) {
	clk := clock.NewManualClock(0)
	rec := newStopRecorder()
	HandleStop(clk, syscall.SIGTERM, rec.hooks(func() { rec.record("cleanup") }))

	clk.AwaitSleepers(1)
	clk.Advance((GracePeriod - ForcedCleanupBudget).Milliseconds())
	code := waitExit(t, rec)

	if code != ExitCodeSigterm {
		t.Fatalf("exit code = %d, want %d", code, ExitCodeSigterm)
	}
	want := []string{"cancel", "close-input", "cleanup", "exit"}
	got := rec.snapshot()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func TestHandleStopExitsEvenWhenCleanupHangs(t *testing.T) {
	clk := clock.NewManualClock(0)
	rec := newStopRecorder()
	release := make(chan struct{})
	defer close(release)
	HandleStop(clk, syscall.SIGINT, rec.hooks(func() { <-release }))

	clk.AwaitSleepers(1)
	clk.Advance((GracePeriod - ForcedCleanupBudget).Milliseconds())
	clk.AwaitSleepers(1)
	clk.Advance(ForcedCleanupBudget.Milliseconds())

	if code := waitExit(t, rec); code != ExitCodeSigint {
		t.Fatalf("exit code = %d, want %d", code, ExitCodeSigint)
	}
}

func TestHandleStopWatchdogLeavesAFlushThatFinishesInTimeAlone(t *testing.T) {
	clk := clock.NewManualClock(0)
	rec := newStopRecorder()
	disarm := HandleStop(clk, syscall.SIGINT, StopHooks{
		Cancel: func() {},
		Exit:   func(code int) { rec.exited <- code },
	})
	clk.AwaitSleepers(1)

	disarm()
	clk.AwaitIdle()
	clk.Advance(GracePeriod.Milliseconds())

	select {
	case code := <-rec.exited:
		t.Fatalf("watchdog exited with %d after the flush finished in time", code)
	default:
	}
}

func TestHandleStopWithAParentBudgetWaitsLongerThanThePlainGracePeriod(t *testing.T) {
	clk := clock.NewManualClock(0)
	rec := newStopRecorder()
	hooks := rec.hooks(nil)
	hooks.Grace = ParentGracePeriod
	HandleStop(clk, syscall.SIGINT, hooks)

	clk.AwaitSleepers(1)
	clk.Advance(GracePeriod.Milliseconds())
	select {
	case code := <-rec.exited:
		t.Fatalf("a parent exited with %d after only the plain grace period", code)
	case <-time.After(settleTime):
	}
	clk.Advance((ParentGracePeriod - ForcedCleanupBudget - GracePeriod).Milliseconds())
	if code := waitExit(t, rec); code != ExitCodeSigint {
		t.Fatalf("exit code = %d, want %d", code, ExitCodeSigint)
	}
}

func TestParentGracePeriodOutlastsTheWorkerGracePeriodAndItsKill(t *testing.T) {
	const killEscalation = 2 * time.Second
	if ParentGracePeriod-ForcedCleanupBudget <= GracePeriod+killEscalation {
		t.Fatalf("a parent watchdog at %v fires before a worker is stopped and killed at %v", ParentGracePeriod-ForcedCleanupBudget, GracePeriod+killEscalation)
	}
}

func TestHandleStopToleratesNilOptionalHooks(t *testing.T) {
	clk := clock.NewManualClock(0)
	rec := newStopRecorder()
	HandleStop(clk, syscall.SIGTERM, StopHooks{
		Cancel: func() {},
		Exit:   func(code int) { rec.exited <- code },
	})
	clk.AwaitSleepers(1)
	clk.Advance(GracePeriod.Milliseconds())
	if code := waitExit(t, rec); code != ExitCodeSigterm {
		t.Fatalf("exit code = %d, want %d", code, ExitCodeSigterm)
	}
}

func TestInterruptExitCode(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	if _, ok := InterruptExitCode(ctx); ok {
		t.Fatal("live context reported as interrupted")
	}
	cancel(InterruptCause(syscall.SIGTERM))
	if got, ok := InterruptExitCode(ctx); !ok || got != ExitCodeSigterm {
		t.Errorf("InterruptExitCode = %d, %v, want %d, true", got, ok, ExitCodeSigterm)
	}

	plain, cancelPlain := context.WithCancel(context.Background())
	cancelPlain()
	if _, ok := InterruptExitCode(plain); ok {
		t.Error("plain cancellation reported as interrupted")
	}
}
