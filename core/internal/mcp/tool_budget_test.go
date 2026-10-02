package mcp

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/storage"
)

func TestBudgetContext_ExpiresWithDeadlineExceededWhenTheBudgetElapses(t *testing.T) {
	clk := clock.NewManualClock(0)
	b, stop := newBudgetContext(context.Background(), clk, 30_000)
	defer stop()
	clk.AwaitSleepers(1)

	clk.Advance(29_999)
	if err := b.Err(); err != nil {
		t.Fatalf("Err() = %v before the budget elapsed", err)
	}
	clk.Advance(1)
	<-b.Done()

	if !errors.Is(b.Err(), context.DeadlineExceeded) {
		t.Fatalf("Err() = %v, want DeadlineExceeded", b.Err())
	}
}

func TestBudgetContext_APausedBudgetDoesNotExpireAndResumesWithAFullBudget(t *testing.T) {
	clk := clock.NewManualClock(0)
	b, stop := newBudgetContext(context.Background(), clk, 30_000)
	defer stop()
	clk.AwaitSleepers(1)

	resume := pauseToolBudget(b)
	clk.Advance(40_000)
	if err := b.Err(); err != nil {
		t.Fatalf("Err() = %v, want a paused budget to ignore 40 seconds of acquisition", err)
	}
	if _, has := b.Deadline(); has {
		t.Fatal("Deadline() reports a deadline while paused")
	}

	resume()
	clk.AwaitSleepers(1)
	clk.Advance(29_999)
	if err := b.Err(); err != nil {
		t.Fatalf("Err() = %v, want the resumed budget to start over in full", err)
	}
	clk.Advance(1)
	<-b.Done()
	if !errors.Is(b.Err(), context.DeadlineExceeded) {
		t.Fatalf("Err() = %v, want DeadlineExceeded once the resumed budget elapsed", b.Err())
	}
}

func TestBudgetContext_ParentCancellationEndsIt(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	clk := clock.NewManualClock(0)
	b, stop := newBudgetContext(parent, clk, 30_000)
	defer stop()

	cancel()
	<-b.Done()

	if !errors.Is(b.Err(), context.Canceled) {
		t.Fatalf("Err() = %v, want Canceled", b.Err())
	}
}

func TestBudgetContext_StopReleasesTheTimer(t *testing.T) {
	clk := clock.NewManualClock(0)
	b, stop := newBudgetContext(context.Background(), clk, 30_000)
	clk.AwaitSleepers(1)

	stop()

	clk.AwaitIdle()
	if !errors.Is(b.Err(), context.Canceled) {
		t.Fatalf("Err() = %v, want Canceled after stop", b.Err())
	}
}

func TestBudgetContext_ReportsARemainingBudgetAsADeadlineWhileRunning(t *testing.T) {
	clk := clock.NewManualClock(0)
	b, stop := newBudgetContext(context.Background(), clk, 30_000)
	defer stop()
	if _, has := b.Deadline(); !has {
		t.Fatal("Deadline() reports none for a running budget")
	}
}

func TestPauseToolBudget_OutsideAToolCallIsANoOp(t *testing.T) {
	pauseToolBudget(context.Background())()
}

// slowAcquireFactory advances the shared clock while it builds, the way
// a runner launch takes wall-clock time, and only once the call's time
// budget is running, so a budget that is not paused is certain to see
// the time pass.
type slowAcquireFactory struct {
	clk       *clock.ManualClock
	elapsedMs int64
	inner     device.Factory
	// waitForCancel makes Build return when its context ends, the way a
	// real launch honors cancellation.
	waitForCancel bool
}

func (f slowAcquireFactory) Build(ctx context.Context, d device.Descriptor) (device.Built, error) {
	if b, ok := ctx.Value(budgetContextKey{}).(*budgetContext); ok {
		if _, running := b.Deadline(); running {
			f.clk.AwaitSleepers(1)
		}
	}
	f.clk.Advance(f.elapsedMs)
	if f.waitForCancel {
		// A timer that is still registering misses one Advance, so time
		// keeps moving until the budget has ended the context.
		for ctx.Err() == nil {
			f.clk.Advance(f.elapsedMs)
			runtime.Gosched()
		}
		return device.Built{}, ctx.Err()
	}
	return f.inner.Build(ctx, d)
}

func TestSelectDevice_AnAcquisitionLongerThanTheToolBudgetIsNotCutOff(t *testing.T) {
	const acquireMs = 40_000
	clk := clock.NewManualClock(0)
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:      device.NewStaticProbe(),
		Factory:    slowAcquireFactory{clk: clk, elapsedMs: acquireMs, inner: singleMockDriverFactory{driver: drv}},
		Storage:    storage.NewInMemoryStorage(),
		Clock:      clk,
		TimerClock: clk,
		Timeouts:   session.Timeouts{McpIdleMs: 600_000, OpenRunIdleMs: 600_000, DeviceAcquireMs: 120_000},
	})

	result, decoded := callTool(t, cs, ctx, "select_device", map[string]any{"platform": "android", "id": "emulator-5554"})

	if result.IsError || decoded["ok"] != true {
		t.Fatalf("select_device after a %d ms acquire = isError %v %v, want it to succeed past the %d ms tool budget",
			acquireMs, result.IsError, decoded, defaultToolTimeoutMs)
	}
}

func TestSelectDevice_AnAcquisitionBeyondTheAcquireBudgetTimesOut(t *testing.T) {
	clk := clock.NewManualClock(0)
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:      device.NewStaticProbe(),
		Factory:    slowAcquireFactory{clk: clk, elapsedMs: 130_000, waitForCancel: true, inner: singleMockDriverFactory{driver: drv}},
		Storage:    storage.NewInMemoryStorage(),
		Clock:      clk,
		TimerClock: clk,
		Timeouts:   session.Timeouts{McpIdleMs: 600_000, OpenRunIdleMs: 600_000, DeviceAcquireMs: 120_000},
	})

	result, decoded := callTool(t, cs, ctx, "select_device", map[string]any{"platform": "android", "id": "emulator-5554"})

	if decoded["ok"] != false || decoded["code"] != "TOOL_TIMEOUT" {
		t.Fatalf("select_device = isError %v %v, want TOOL_TIMEOUT once the acquire budget is spent", result.IsError, decoded)
	}
}
