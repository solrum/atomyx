package shutdown

import (
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
)

const (
	testGraceMs = int64(30)
	testTimeout = 10 * time.Second
)

func TestBoundedReportsCompletion(t *testing.T) {
	ran := false
	if !Bounded(clock.SystemClock{}, time.Second, func() { ran = true }) {
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
	go func() { result <- Bounded(clk, time.Second, func() { <-release }) }()
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
