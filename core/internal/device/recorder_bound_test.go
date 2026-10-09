package device

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

const (
	boundTestDeadline = 300 * time.Millisecond
	// boundTestSlack is how far past the deadline a bounded call may
	// return. It stays far below every unbounded wait the recorders have.
	boundTestSlack = 3 * time.Second
)

// withinBound runs call and fails the test when it does not return in
// boundTestSlack.
func withinBound(t *testing.T, what string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		call()
	}()
	select {
	case <-done:
	case <-time.After(boundTestSlack):
		t.Fatalf("%s outlived its deadline", what)
	}
}

func TestAndroidRecorder_StopAndDiscardHonourTheDeadlineWhenAdbHangs(t *testing.T) {
	h := newRecorderHarness(t)
	rec, _ := startAndroid(t, h, 111)
	h.hang.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), boundTestDeadline)
	defer cancel()

	withinBound(t, "Stop and Discard", func() {
		if _, err := rec.Stop(ctx); err == nil {
			t.Error("Stop succeeded against a hung adb")
		}
		_ = rec.Discard(ctx)
	})
	if !h.procs[0].Killed() {
		t.Fatal("the host adb process was left running")
	}
}

func TestAndroidRecorder_StopStopsWaitingForAFinalizeThatNeverComes(t *testing.T) {
	h := newRecorderHarness(t)
	rec, _ := startAndroid(t, h, 111)
	h.noFinalize.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), boundTestDeadline)
	defer cancel()

	withinBound(t, "Stop", func() {
		if _, err := rec.Stop(ctx); err == nil {
			t.Error("Stop succeeded without a finalized file")
		}
	})
}

func TestAndroidRecorder_StartBoundsThePrepareStep(t *testing.T) {
	h := newRecorderHarness(t)
	h.hang.Store(true)

	withinBound(t, "Start", func() {
		recorder := adbRecorder{deps: h.deps(), wait: h.wait, controlTimeout: boundTestDeadline}
		rec, err := recorder.Start(context.Background(), androidDesc, t.TempDir(), "video-1")
		if err == nil || rec != nil {
			t.Errorf("Start = %v, %v, want an error", rec, err)
		}
	})
}

// hungInterrupt is a recorder process whose Interrupt blocks until it is
// killed, the way a simctl recorder stuck in the kernel does.
type hungInterrupt struct {
	*process.FakeBackgroundProcess
	once sync.Once
	gone chan struct{}
}

func (p *hungInterrupt) Interrupt(time.Duration) error {
	<-p.gone
	return nil
}

func (p *hungInterrupt) Kill() error {
	p.once.Do(func() { close(p.gone) })
	return p.FakeBackgroundProcess.Kill()
}

// singleBackground starts the one process it holds.
type singleBackground struct{ proc process.BackgroundProcess }

func (b singleBackground) Start(context.Context, string, ...string) (process.BackgroundProcess, error) {
	return b.proc, nil
}

func TestSimulatorRecorder_StopAndDiscardHonourTheDeadlineWhenTheRecorderHangs(t *testing.T) {
	h := newRecorderHarness(t)
	hung := &hungInterrupt{FakeBackgroundProcess: process.NewFakeBackgroundProcess(), gone: make(chan struct{})}
	deps := RecorderDeps{Runner: h, Background: singleBackground{hung}}
	rec, err := simulatorRecorder{deps: deps, wait: h.wait}.Start(context.Background(), simulatorDesc, t.TempDir(), "video-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.awaitIdle()
	ctx, cancel := context.WithTimeout(context.Background(), boundTestDeadline)
	defer cancel()

	withinBound(t, "Stop and Discard", func() {
		if _, err := rec.Stop(ctx); err == nil {
			t.Error("Stop succeeded against a hung recorder")
		}
		_ = rec.Discard(ctx)
	})
}

func TestRotation_StopAbortsARotateInFlightWhenItsDeadlineEnds(t *testing.T) {
	entered := make(chan struct{})
	rot := startRotation(context.Background(), onceWaiter(), func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}, func(error) {})
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), boundTestDeadline)
	defer cancel()

	withinBound(t, "stop", func() {
		rot.stop(ctx)
	})
}

// onceWaiter reports one due rotation, then waits for its context to end.
func onceWaiter() rotationWaiter {
	var fired atomic.Bool
	return func(ctx context.Context) bool {
		if fired.CompareAndSwap(false, true) {
			return true
		}
		<-ctx.Done()
		return false
	}
}
