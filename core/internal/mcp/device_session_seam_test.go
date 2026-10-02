package mcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
)

const disconnectDeadline = time.Millisecond

// onePhoneIdentity reports every Android serial as the same hardware,
// like one phone reached over USB and over Wi-Fi.
type onePhoneIdentity struct{ hardwareID string }

func (r onePhoneIdentity) Resolve(_ context.Context, d device.Descriptor) (device.Identity, error) {
	return device.Identity{Platform: d.Platform, HardwareID: r.hardwareID}, nil
}

// failNextBuildFactory fails the next Build when failNext is set, and
// builds like inner otherwise.
type failNextBuildFactory struct {
	inner    device.Factory
	failNext atomic.Bool
}

func (f *failNextBuildFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	if f.failNext.CompareAndSwap(true, false) {
		return device.Built{}, errors.New("build refused")
	}
	return f.inner.Build(ctx, desc)
}

func (f *lifecycleFixture) newDevicesWith(factory device.Factory, deps session.Deps) *DeviceSession {
	deps.Leases = f.leases
	deps.Clock = clock.SystemClock{}
	return NewDeviceSession(DeviceSessionDeps{
		Factory: factory, Probe: device.NewStaticProbe(), Clock: f.clk, TimerClock: f.clk, Logger: logging.NoopLogger{},
		Session:  deps,
		Timeouts: session.Timeouts{McpIdleMs: testIdleMs, OpenRunIdleMs: testOpenRunMs, DeviceAcquireMs: 120_000},
		Runs:     f.runs,
	})
}

func TestSelectOfOnePhoneThroughTwoSerialsTearsTheFirstDown(t *testing.T) {
	f := newLifecycleFixture(t)
	ds := f.newDevicesWith(f.factory, session.Deps{Identity: onePhoneIdentity{hardwareID: "phone-1"}})
	t.Cleanup(ds.Close)
	if err := f.selectDevice(ds); err != nil {
		t.Fatal(err)
	}

	second, err := ds.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformAndroid, ID: "B"})
	if err != nil {
		t.Fatalf("Select() of the same phone through another serial = %v, want the first session torn down first", err)
	}

	if second.ID != "B" || f.factory.builds() != 2 {
		t.Fatalf("active = %+v builds = %d, want a second build for serial B", second, f.factory.builds())
	}
	select {
	case <-f.factory.disconnected:
	default:
		t.Fatal("the session on serial A was not torn down")
	}
	held, err := f.leases.Held(context.Background(), "android", "phone-1")
	if err != nil || !held {
		t.Fatalf("lease held = %v err = %v, want the phone leased by the new session", held, err)
	}
}

func TestDisconnectWaitingForATransitionReturnsTheContextError(t *testing.T) {
	f := newLifecycleFixture(t)
	ds, pf, ensured := idleReleasedWithPausedRebuild(t, f)

	ctx, cancel := context.WithTimeout(context.Background(), disconnectDeadline)
	defer cancel()
	err := ds.Disconnect(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Disconnect() = %v, want the deadline error while the re-acquire builds", err)
	}

	pf.let()
	if err := <-ensured; err != nil {
		t.Fatalf("Ensure() = %v, want the re-acquire to finish untouched by the timed out Disconnect", err)
	}
	if ds.Current() == nil || f.state(ds) != holderIdle {
		t.Fatalf("current = %v state = %s, want the re-acquired device held", ds.Current(), f.state(ds))
	}
	if err := ds.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ds.Current() != nil || f.state(ds) != holderUnselected {
		t.Fatalf("current = %v state = %s, want unselected after a Disconnect that was allowed to wait", ds.Current(), f.state(ds))
	}
}

func TestFailedSelectOfTheHeldDeviceLeavesItRemembered(t *testing.T) {
	f := newLifecycleFixture(t)
	factory := &failNextBuildFactory{inner: f.factory}
	ds := f.newDevicesWith(factory, session.Deps{})
	t.Cleanup(ds.Close)
	if err := f.selectDevice(ds); err != nil {
		t.Fatal(err)
	}

	factory.failNext.Store(true)
	if err := f.selectDevice(ds); err == nil {
		t.Fatal("Select() succeeded although the build failed")
	}

	if got := f.state(ds); got != holderReleased {
		t.Fatalf("state = %s, want released because the descriptor stays remembered", got)
	}
	active, err := ds.Ensure(context.Background())
	if err != nil || active.ID != "A" {
		t.Fatalf("Ensure() = %+v, %v, want the remembered device taken again", active, err)
	}
}
