package session_test

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

type funcPreparer func(ctx context.Context, desc device.Descriptor) error

func (f funcPreparer) Prepare(ctx context.Context, desc device.Descriptor) error { return f(ctx, desc) }

var iosDescriptor = device.Descriptor{ID: "UDID-1", Platform: device.PlatformIOS, Kind: device.KindDevice}

func TestPrepareWithoutAPreparerDoesNothing(t *testing.T) {
	if err := session.Prepare(context.Background(), session.Deps{}, iosDescriptor); err != nil {
		t.Fatalf("Prepare() error = %v, want nil", err)
	}
}

func TestPreparePassesTheDescriptorAndTheError(t *testing.T) {
	want := errors.New("build failed")
	var got device.Descriptor
	deps := session.Deps{Preparer: funcPreparer(func(_ context.Context, desc device.Descriptor) error {
		got = desc
		return want
	})}

	err := session.Prepare(context.Background(), deps, iosDescriptor)

	if !errors.Is(err, want) {
		t.Fatalf("Prepare() error = %v, want the preparer's error", err)
	}
	if got != iosDescriptor {
		t.Fatalf("preparer saw %+v, want %+v", got, iosDescriptor)
	}
}

func TestPrepareReportsRunnerBuildTimeoutWhenTheBuildBudgetElapses(t *testing.T) {
	clk := clock.NewManualClock(0)
	deps := session.Deps{Clock: clk, Preparer: funcPreparer(func(ctx context.Context, _ device.Descriptor) error {
		clk.AwaitSleepers(1)
		clk.Advance(device.DefaultRunnerBuildTimeoutMs)
		<-ctx.Done()
		return ctx.Err()
	})}

	err := session.Prepare(context.Background(), deps, iosDescriptor)

	if code, _, remedy := diag.Describe(err); code != diag.CodeRunnerBuildTimeout || remedy == "" {
		t.Fatalf("Prepare() code = %q remedy = %q, want %s with a remedy", code, remedy, diag.CodeRunnerBuildTimeout)
	}
}

func TestPrepareReportsACanceledCallerAsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	deps := session.Deps{Clock: clock.NewManualClock(0), Preparer: funcPreparer(func(ctx context.Context, _ device.Descriptor) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})}

	err := session.Prepare(ctx, deps, iosDescriptor)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Prepare() error = %v, want context.Canceled", err)
	}
	if code, _, _ := diag.Describe(err); code == diag.CodeRunnerBuildTimeout {
		t.Fatalf("a canceled caller was reported as %s", code)
	}
}
