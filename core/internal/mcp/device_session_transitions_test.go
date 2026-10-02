package mcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/device"
)

const transitionsFailsafe = 10 * time.Second

// gatedPreparer models a runner build the test controls. While armed it
// reports each start on started and blocks until release is closed or
// its context ends. A context that ends makes it fail the way a killed
// build does, with an error that is not a context error.
type gatedPreparer struct {
	armed   atomic.Bool
	started chan struct{}
	release chan struct{}
}

var errBuildKilled = errors.New("the build process was killed")

func newGatedPreparer() *gatedPreparer {
	return &gatedPreparer{started: make(chan struct{}, 8), release: make(chan struct{})}
}

func (p *gatedPreparer) Prepare(ctx context.Context, _ device.Descriptor) error {
	if !p.armed.Load() {
		return nil
	}
	p.started <- struct{}{}
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return errBuildKilled
	}
}

func (p *gatedPreparer) awaitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-p.started:
	case <-time.After(transitionsFailsafe):
		t.Fatal("the build never started")
	}
}

// releasedWithGate selects device A on a session whose preparer the test
// controls, then lets the idle timer release it.
func releasedWithGate(t *testing.T, f *lifecycleFixture) (*DeviceSession, *gatedPreparer) {
	t.Helper()
	gate := newGatedPreparer()
	ds := f.newDevicesPreparing("atomyx-mcp", f.factory, gate)
	t.Cleanup(ds.Close)
	if err := f.selectDevice(ds); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(ds)
	gate.armed.Store(true)
	return ds, gate
}

func awaitResult[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(transitionsFailsafe):
		t.Fatal("the call never returned")
		panic("unreachable")
	}
}

func TestEnsureBuildsOutsideAcquireMu(t *testing.T) {
	f := newLifecycleFixture(t)
	ds, gate := releasedWithGate(t, f)
	done := make(chan error, 1)
	go func() {
		_, err := ds.Ensure(context.Background())
		done <- err
	}()
	gate.awaitStarted(t)

	if !ds.acquireMu.TryLock() {
		t.Fatal("acquireMu is held while the runner builds, so an idle release or a lost device would wait for the build")
	}
	ds.acquireMu.Unlock()

	close(gate.release)
	if err := awaitResult(t, done); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if ds.Current() == nil {
		t.Fatal("Current() is nil after Ensure took the device again")
	}
}

func TestEnsureSeesADisconnectMadeWhileItBuilt(t *testing.T) {
	f := newLifecycleFixture(t)
	ds, gate := releasedWithGate(t, f)
	done := make(chan error, 1)
	go func() {
		_, err := ds.Ensure(context.Background())
		done <- err
	}()
	gate.awaitStarted(t)

	_ = ds.Disconnect(context.Background())
	close(gate.release)

	if err := awaitResult(t, done); !errors.Is(err, errNoActiveDevice) {
		t.Fatalf("Ensure() error = %v, want errNoActiveDevice because the device was disconnected while it built", err)
	}
	if ds.Current() != nil {
		t.Fatal("Ensure took a device the agent disconnected")
	}
}

func TestSelectCanceledWhileBuildingReturnsTheContextError(t *testing.T) {
	f := newLifecycleFixture(t)
	gate := newGatedPreparer()
	gate.armed.Store(true)
	ds := f.newDevicesPreparing("atomyx-mcp", f.factory, gate)
	t.Cleanup(ds.Close)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ds.Select(ctx, SelectDeviceInput{Platform: device.PlatformAndroid, ID: "A"})
		done <- err
	}()
	gate.awaitStarted(t)

	cancel()

	err := awaitResult(t, done)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Select() error = %v, want context.Canceled", err)
	}
	if errors.Is(err, errBuildKilled) {
		t.Fatalf("Select() error = %v, want the cancellation, not the failure of the killed build", err)
	}
}

func TestCloseStopsASelectStillBuilding(t *testing.T) {
	f := newLifecycleFixture(t)
	gate := newGatedPreparer()
	gate.armed.Store(true)
	ds := f.newDevicesPreparing("atomyx-mcp", f.factory, gate)
	done := make(chan error, 1)
	go func() {
		_, err := ds.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformAndroid, ID: "A"})
		done <- err
	}()
	gate.awaitStarted(t)

	closed := make(chan struct{})
	go func() {
		ds.Close()
		close(closed)
	}()

	awaitResult(t, closed)
	if err := awaitResult(t, done); !errors.Is(err, errSessionClosed) {
		t.Fatalf("Select() error = %v, want errSessionClosed", err)
	}
	if f.leaseHeld() {
		t.Fatal("the lease is held after Close stopped the selection")
	}
}

func TestSelectRacingTheIdleTimerKeepsTheSessionConsistent(t *testing.T) {
	const rounds = 20
	f := newLifecycleFixture(t)
	stopDrain := make(chan struct{})
	var drain sync.WaitGroup
	drain.Add(1)
	go func() {
		defer drain.Done()
		for {
			select {
			case <-f.factory.disconnected:
			case <-stopDrain:
				return
			}
		}
	}()
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}

	for range rounds {
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = f.selectDevice(f.devices)
		}()
		go func() {
			defer wg.Done()
			f.clk.Advance(testIdleMs)
		}()
		go func() {
			defer wg.Done()
			_, _ = f.devices.Ensure(context.Background())
		}()
		wg.Wait()

		f.devices.mu.Lock()
		consistent := (f.devices.sess == nil) == (f.devices.active == nil)
		f.devices.mu.Unlock()
		if !consistent {
			t.Fatal("the session holds a device without an active record, or the reverse")
		}
	}

	f.devices.Close()
	close(stopDrain)
	drain.Wait()
	if f.leaseHeld() {
		t.Fatal("the device lease is still held after Close")
	}
	if f.devices.Current() != nil {
		t.Fatal("Current() is not nil after Close")
	}
}
