package mcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pausingFactory builds like its inner factory except the second build,
// the re-acquire after an idle release, which waits until released or
// until its context ends.
type pausingFactory struct {
	inner    *lifecycleFactory
	calls    atomic.Int32
	paused   chan struct{}
	release  chan struct{}
	released atomic.Bool
}

func newPausingFactory(inner *lifecycleFactory) *pausingFactory {
	return &pausingFactory{inner: inner, paused: make(chan struct{}), release: make(chan struct{})}
}

func (p *pausingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(p).WithDriverWrap(wrap)
}

func (p *pausingFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	if p.calls.Add(1) == 2 {
		close(p.paused)
		select {
		case <-p.release:
		case <-ctx.Done():
			return device.Built{}, ctx.Err()
		}
	}
	return p.inner.Build(ctx, desc)
}

func (p *pausingFactory) let() {
	p.released.Store(true)
	close(p.release)
}

// idleReleasedWithPausedRebuild returns a session whose device was
// released for idleness and whose re-acquire is paused inside Build.
func idleReleasedWithPausedRebuild(t *testing.T, f *lifecycleFixture) (*DeviceSession, *pausingFactory, <-chan error) {
	t.Helper()
	pf := newPausingFactory(f.factory)
	ds := f.newDevices("atomyx-mcp", pf)
	t.Cleanup(ds.Close)
	if err := f.selectDevice(ds); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(ds)

	ensured := make(chan error, 1)
	go func() {
		_, err := ds.Ensure(context.Background())
		ensured <- err
	}()
	<-pf.paused
	return ds, pf, ensured
}

func TestDeviceSession_DisconnectDuringAReacquireWaitsForItAndTearsDownTheResult(t *testing.T) {
	f := newLifecycleFixture(t)
	ds, pf, ensured := idleReleasedWithPausedRebuild(t, f)

	returnedAfterRelease := make(chan bool, 1)
	go func() {
		_ = ds.Disconnect(context.Background())
		returnedAfterRelease <- pf.released.Load()
	}()
	pf.let()

	if err := <-ensured; err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !<-returnedAfterRelease {
		t.Fatal("Disconnect() returned while the re-acquire was still building")
	}
	if current := ds.Current(); current != nil {
		t.Fatalf("Current() = %+v, want nil after Disconnect", current)
	}
	if f.leaseHeld() {
		t.Fatal("the lease is held after Disconnect")
	}
	select {
	case <-f.factory.disconnected:
	default:
		t.Fatal("the re-acquired driver was not disconnected")
	}
}

func TestDeviceSession_CloseDuringAPausedBuildFinishesAndFreesTheLease(t *testing.T) {
	f := newLifecycleFixture(t)
	ds, _, ensured := idleReleasedWithPausedRebuild(t, f)

	closed := make(chan struct{})
	go func() {
		ds.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(transitionsFailsafe):
		t.Fatal("Close() did not finish while a build waited on its context")
	}
	if err := <-ensured; err == nil {
		t.Fatal("Ensure() succeeded on a closed session")
	}
	if f.leaseHeld() {
		t.Fatal("the lease is held after Close")
	}
}

func lifecycleServer(t *testing.T, f *lifecycleFixture) (*mcp.ClientSession, context.Context) {
	t.Helper()
	return testServer(t, ServerDeps{
		Probe:      device.NewStaticProbe(),
		Factory:    f.factory,
		Storage:    storage.NewInMemoryStorage(),
		Clock:      f.clk,
		TimerClock: f.clk,
		Session:    session.Deps{Leases: f.leases, Clock: clock.SystemClock{}, Command: "atomyx-mcp", Role: "mcp"},
		Timeouts:   session.Timeouts{McpIdleMs: testIdleMs, OpenRunIdleMs: testOpenRunMs, DeviceAcquireMs: contractAcquireBudgetMs},
	})
}

func TestCheckHealth_ReportsIdleForAHeldDeviceNoCallIsUsing(t *testing.T) {
	f := newLifecycleFixture(t)
	cs, ctx := lifecycleServer(t, f)
	callTool(t, cs, ctx, "select_device", map[string]any{"platform": "android", "id": "A"})

	_, report := callTool(t, cs, ctx, "check_health", map[string]any{})

	if report["state"] != "idle" {
		t.Fatalf("check_health state = %v, want idle", report["state"])
	}
}

func TestCheckHealth_PollingDoesNotKeepTheDeviceFromIdleRelease(t *testing.T) {
	f := newLifecycleFixture(t)
	cs, ctx := lifecycleServer(t, f)
	callTool(t, cs, ctx, "select_device", map[string]any{"platform": "android", "id": "A"})
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs - 1)

	callTool(t, cs, ctx, "check_health", map[string]any{})
	callTool(t, cs, ctx, "list_devices", map[string]any{})
	f.clk.Advance(1)

	select {
	case <-f.factory.disconnected:
	case <-time.After(transitionsFailsafe):
		t.Fatal("observe-only calls restarted the idle timer, so the device was not released on time")
	}
}

func TestReacquireOnTheSameRunnerReportsNoRelaunch(t *testing.T) {
	f := newLifecycleFixture(t)
	f.factory.onBuild = func(_ context.Context, d *lifecycleDriver) {
		d.bundleID, d.runnerID = "com.example.app", "runner-1"
	}
	cs, ctx := lifecycleServer(t, f)
	callTool(t, cs, ctx, "select_device", map[string]any{"platform": "android", "id": "A"})
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	<-f.factory.disconnected

	result, _ := callTool(t, cs, ctx, "get_ui_tree", map[string]any{})

	if len(result.Content) != 1 {
		t.Fatalf("get_ui_tree content blocks = %d, want only the tool's own block when the runner is unchanged", len(result.Content))
	}
}
