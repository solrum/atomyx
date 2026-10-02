package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/storage"
)

const (
	testIdleMs    int64 = 1_000
	testOpenRunMs int64 = 5_000
)

// lifecycleDriver is the in-memory driver with the launched-app
// accessors an iOS driver has, and a signal for each Disconnect.
type lifecycleDriver struct {
	*mockdriver.Driver
	factory *lifecycleFactory

	bundleID string
	runnerID string

	restoredBundle string
	restoredRunner string
}

func (d *lifecycleDriver) Disconnect() error {
	err := d.Driver.Disconnect()
	d.factory.disconnected <- struct{}{}
	return err
}

func (d *lifecycleDriver) LaunchedApp() (string, string) { return d.bundleID, d.runnerID }

func (d *lifecycleDriver) RestoreLaunchedApp(bundleID, fromRunnerID string) bool {
	d.restoredBundle, d.restoredRunner = bundleID, fromRunnerID
	return true
}

// lifecycleFactory builds lifecycleDrivers and records them.
type lifecycleFactory struct {
	disconnected chan struct{}
	onBuild      func(ctx context.Context, d *lifecycleDriver)

	mu      sync.Mutex
	drivers []*lifecycleDriver
}

func newLifecycleFactory() *lifecycleFactory {
	return &lifecycleFactory{disconnected: make(chan struct{}, 32)}
}

func (f *lifecycleFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	drv := &lifecycleDriver{Driver: mockdriver.New(), factory: f}
	drv.StageHierarchy(sampleTree())
	if f.onBuild != nil {
		f.onBuild(ctx, drv)
	}
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	f.mu.Lock()
	f.drivers = append(f.drivers, drv)
	f.mu.Unlock()
	return device.Built{Driver: drv, Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}})}, nil
}

func (f *lifecycleFactory) builds() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.drivers)
}

func (f *lifecycleFactory) driver(i int) *lifecycleDriver {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drivers[i]
}

// gatedLeaseClock is the clock a lease manager runs on in these tests.
// A short sleep, the acquire retry, returns at once. The long sleep of
// the inode watch waits for fire, so a test decides when the watch
// looks at the lock file.
type gatedLeaseClock struct {
	mu   sync.Mutex
	now  int64
	gate chan struct{}
}

func newGatedLeaseClock() *gatedLeaseClock { return &gatedLeaseClock{gate: make(chan struct{})} }

func (c *gatedLeaseClock) NowMs() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *gatedLeaseClock) Sleep(ctx context.Context, ms int64) error {
	if ms >= lease.InodeRecheckPeriod.Milliseconds() {
		select {
		case <-c.gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.mu.Lock()
	c.now += ms
	c.mu.Unlock()
	return ctx.Err()
}

func (c *gatedLeaseClock) fire() { c.gate <- struct{}{} }

type lifecycleFixture struct {
	t        *testing.T
	clk      *clock.ManualClock
	leaseClk *gatedLeaseClock
	leases   *lease.Manager
	factory  *lifecycleFactory
	runStore *storage.RunStore
	store    storage.Storage
	runs     *runTracker
	devices  *DeviceSession
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	root := t.TempDir()
	leaseClk := newGatedLeaseClock()
	leases, err := lease.NewManager(lease.Environment{
		GOOS:          "linux",
		Getenv:        func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": root}[k] },
		UserCacheDir:  func() (string, error) { return root, nil },
		Getuid:        os.Getuid,
		IsLocalFS:     func(string) (bool, error) { return true, nil },
		FallbackRoot:  root,
		MaxSocketPath: 4096,
	}, leaseClk)
	if err != nil {
		t.Fatal(err)
	}
	f := &lifecycleFixture{
		t: t, clk: clock.NewManualClock(0), leaseClk: leaseClk, leases: leases,
		factory: newLifecycleFactory(), runStore: storage.NewRunStore(), store: storage.NewInMemoryStorage(),
	}
	f.runs = &runTracker{runs: f.runStore, storage: f.store, clock: f.clk, logger: logging.NoopLogger{}}
	f.devices = f.newDevices("atomyx-mcp", f.factory)
	t.Cleanup(f.devices.Close)
	return f
}

// newDevices builds another DeviceSession over the same lease directory,
// standing in for another Atomyx process.
func (f *lifecycleFixture) newDevices(command string, factory device.Factory) *DeviceSession {
	return NewDeviceSession(DeviceSessionDeps{
		Factory: factory, Probe: device.NewStaticProbe(), Clock: f.clk, TimerClock: f.clk, Logger: logging.NoopLogger{},
		Session:  session.Deps{Leases: f.leases, Clock: clock.SystemClock{}, Command: command},
		Timeouts: session.Timeouts{McpIdleMs: testIdleMs, OpenRunIdleMs: testOpenRunMs, DeviceAcquireMs: 120_000},
		Runs:     f.runs,
	})
}

func (f *lifecycleFixture) selectDevice(ds *DeviceSession) error {
	f.t.Helper()
	_, err := ds.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformAndroid, ID: "A"})
	return err
}

// awaitRelease waits until the session finished giving its device up:
// the driver disconnected, and the release holds acquireMu until it is
// done.
func (f *lifecycleFixture) awaitRelease(ds *DeviceSession) {
	f.t.Helper()
	<-f.factory.disconnected
	ds.acquireMu.Lock()
	ds.acquireMu.Unlock() //nolint:staticcheck // the empty section is the barrier
}

func (f *lifecycleFixture) state(ds *DeviceSession) holderState {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.state
}

func (f *lifecycleFixture) leaseHeld() bool {
	f.t.Helper()
	status, err := f.leases.Probe(context.Background(), lease.KeyFor("android", "A"))
	if err != nil {
		f.t.Fatal(err)
	}
	return status.Held
}

func codeOfErr(err error) string {
	code, _, _ := diag.Describe(err)
	return code
}

func TestDeviceSession_SelectTakesTheLeaseAndASecondProcessGetsDeviceHeld(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	if !f.leaseHeld() {
		t.Fatal("the device lease is not held after select_device")
	}

	other := f.newDevices("atomyx run", newLifecycleFactory())
	t.Cleanup(other.Close)
	err := f.selectDevice(other)

	code, message, remedy := diag.Describe(err)
	if code != diag.CodeDeviceHeld {
		t.Fatalf("code = %s (%v), want DEVICE_HELD for a session that never owned the device", code, err)
	}
	if !strings.Contains(message, "atomyx-mcp") || !strings.Contains(remedy, "Stop that process") {
		t.Fatalf("message %q remedy %q, want the holder and the operator remedy", message, remedy)
	}
}

func TestDeviceSession_IdleReleaseGivesTheDeviceUpAndTheNextCallTakesItAgain(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)

	f.clk.Advance(testIdleMs - 1)
	if f.devices.Current() == nil {
		t.Fatal("the device was released before the idle timeout elapsed")
	}
	f.clk.Advance(1)
	f.awaitRelease(f.devices)

	if f.devices.Current() != nil || f.leaseHeld() {
		t.Fatal("the idle timeout left the device or its lease held")
	}
	if got := f.state(f.devices); got != holderReleased {
		t.Fatalf("state = %s, want released, which differs from unselected because the device is remembered", got)
	}
	active, err := f.devices.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v, want the remembered device taken again", err)
	}
	if active.ID != "A" || f.factory.builds() != 2 || !f.leaseHeld() {
		t.Fatalf("active = %+v builds = %d held = %v, want a second build of device A under a fresh lease", active, f.factory.builds(), f.leaseHeld())
	}
}

func TestDeviceSession_TheIdleTimeIsMeasuredFromTheEndOfTheLastCall(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)

	if err := f.devices.BeginCall("tap"); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitIdle()
	f.clk.Advance(10 * testIdleMs)
	if f.devices.Current() == nil {
		t.Fatal("the device was released while a call was in flight")
	}
	f.devices.EndCall()
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs - 1)
	if f.devices.Current() == nil {
		t.Fatal("the idle period did not restart from the end of the call")
	}
	f.clk.Advance(1)
	f.awaitRelease(f.devices)
}

func TestDeviceSession_AReleasedSessionThatFindsTheDeviceHeldReportsDeviceReleasedHeld(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)

	cli := f.newDevices("atomyx run", newLifecycleFactory())
	t.Cleanup(cli.Close)
	if err := f.selectDevice(cli); err != nil {
		t.Fatalf("the operator's process could not take the released device: %v", err)
	}

	_, err := f.devices.Ensure(context.Background())

	code, message, remedy := diag.Describe(err)
	if code != diag.CodeDeviceReleasedHeld {
		t.Fatalf("code = %s (%v), want DEVICE_RELEASED_HELD", code, err)
	}
	if !strings.Contains(message, "atomyx run") || !strings.Contains(remedy, "ask the operator") {
		t.Fatalf("message %q remedy %q, want the holder's command and the agent remedy", message, remedy)
	}
	if got := f.state(f.devices); got != holderReleased {
		t.Fatalf("state = %s, want it unchanged by the failed re-acquire", got)
	}

	cli.Disconnect()
	if _, err := f.devices.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v after the other process let go", err)
	}
}

func TestDeviceSession_AnExplicitDisconnectForgetsTheDevice(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}

	f.devices.Disconnect()
	<-f.factory.disconnected

	if _, err := f.devices.Ensure(context.Background()); codeOfErr(err) != diag.CodeDeviceNotFound {
		t.Fatalf("Ensure() error = %v, want the no-active-device error, not a silent re-acquire", err)
	}
	if got := f.state(f.devices); got != holderUnselected {
		t.Fatalf("state = %s, want unselected", got)
	}
}

func TestDeviceSession_AnOpenRunKeepsTheDeviceUntilTheOpenRunTimeout(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	if err := f.devices.BeginCall("start_run"); err != nil {
		t.Fatal(err)
	}
	f.runStore.Start(storage.StartOptions{Name: "smoke"})
	f.devices.EndCall()
	f.clk.AwaitSleepers(1)

	f.clk.Advance(testIdleMs + 1)
	if f.devices.Current() == nil {
		t.Fatal("the plain idle timeout released a device with a run open")
	}
	if got := f.state(f.devices); got != holderRunning {
		t.Fatalf("state = %s, want running while a run is open", got)
	}
	other := f.newDevices("atomyx run", newLifecycleFactory())
	t.Cleanup(other.Close)
	if _, message, _ := diag.Describe(f.selectDevice(other)); !strings.Contains(message, "running (run open)") {
		t.Fatalf("held message %q, want the holder reported as running with a run open", message)
	}

	f.clk.Advance(testOpenRunMs - testIdleMs - 1)
	if f.devices.Current() == nil {
		t.Fatal("the device was released before the open-run timeout elapsed")
	}
	f.clk.Advance(1)
	f.awaitRelease(f.devices)

	if f.runStore.Current() != nil {
		t.Fatal("the open run is still open after the open-run timeout")
	}
	persisted, ok := f.runsPersisted()
	if !ok || persisted.Status != storage.RunStatusCancelled {
		t.Fatalf("persisted run = %+v (%v), want it closed as cancelled", persisted, ok)
	}
}

func (f *lifecycleFixture) runsPersisted() (storage.PersistedRun, bool) {
	keys, err := f.store.List("runs/")
	if err != nil || len(keys) == 0 {
		return storage.PersistedRun{}, false
	}
	var run storage.PersistedRun
	if found, err := f.store.Load(keys[0], &run); err != nil || !found {
		f.t.Fatalf("load %s: found=%v err=%v", keys[0], found, err)
	}
	return run, true
}

func TestDeviceSession_EveryCallOnTheOpenRunRestartsTheOpenRunTimeout(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.runStore.Start(storage.StartOptions{Name: "smoke"})
	if err := f.devices.BeginCall("tap"); err != nil {
		t.Fatal(err)
	}
	f.devices.EndCall()
	f.clk.AwaitSleepers(1)

	f.clk.Advance(testOpenRunMs - 1)
	if err := f.devices.BeginCall("tap"); err != nil {
		t.Fatal(err)
	}
	f.devices.EndCall()
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testOpenRunMs - 1)
	if f.devices.Current() == nil {
		t.Fatal("the open-run timeout did not restart on the later call")
	}
	f.clk.Advance(1)
	f.awaitRelease(f.devices)
}

func TestDeviceSession_AfterTheOpenRunTimeoutTheRunScopedToolsReportRunExpired(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	started := f.runStore.Start(storage.StartOptions{Name: "smoke"})
	if err := f.devices.BeginCall("start_run"); err != nil {
		t.Fatal(err)
	}
	f.devices.EndCall()
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testOpenRunMs)
	f.awaitRelease(f.devices)

	if code := codeOfErr(f.devices.BeginCall("tap")); code != diag.CodeRunExpired {
		t.Fatalf("first device call code = %s, want RUN_EXPIRED once", code)
	}
	if err := f.devices.BeginCall("list_devices"); err != nil {
		t.Fatalf("list_devices = %v, want a tool that needs no device never to fire it", err)
	}
	f.devices.EndCall()
	if err := f.devices.BeginCall("tap"); err != nil {
		t.Fatalf("second device call = %v, want it to follow the released path", err)
	}
	f.devices.EndCall()

	for range 2 {
		err := f.devices.BeginCall("finish_run")
		code, message, remedy := diag.Describe(err)
		if code != diag.CodeRunExpired || !strings.Contains(message, started.Run.ID) || !strings.Contains(remedy, "Start a new run") {
			t.Fatalf("finish_run = %s %q %q, want RUN_EXPIRED naming %s every time", code, message, remedy, started.Run.ID)
		}
	}
	if code := codeOfErr(f.devices.BeginCall("report_bug")); code != diag.CodeRunExpired {
		t.Fatalf("report_bug code = %s, want RUN_EXPIRED", code)
	}

	if err := f.devices.BeginCall("start_run"); err != nil {
		t.Fatalf("start_run = %v, want a new run to clear the expiry", err)
	}
	f.devices.EndCall()
	if err := f.devices.BeginCall("finish_run"); err != nil {
		t.Fatalf("finish_run after start_run = %v, want no expiry", err)
	}
	f.devices.EndCall()
}

func TestDeviceSession_ALostLeaseMovesTheSessionToLostAndTheNextCallRecovers(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.runStore.Start(storage.StartOptions{Name: "smoke"})

	lockPath := f.leases.LockPath(lease.KeyFor("android", "A"))
	replacement := filepath.Join(filepath.Dir(lockPath), "replacement")
	if err := os.WriteFile(replacement, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, lockPath); err != nil {
		t.Fatal(err)
	}
	intruder, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(intruder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	f.leaseClk.fire()
	f.awaitRelease(f.devices)

	if got := f.state(f.devices); got != holderLost || f.devices.Current() != nil {
		t.Fatalf("state = %s current = %v, want lost with no device bound", got, f.devices.Current())
	}
	if f.runStore.Current() != nil {
		t.Fatal("the run in flight stayed open after the device was lost")
	}
	if _, err := f.devices.Ensure(context.Background()); codeOfErr(err) != diag.CodeDeviceReleasedHeld {
		t.Fatalf("Ensure() error = %v, want DEVICE_RELEASED_HELD while the lock is held elsewhere", err)
	}
	_ = intruder.Close()
	if _, err := f.devices.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v once the lock was free again", err)
	}
}

func TestDeviceSession_TheLaunchedAppCarriesAcrossAReleaseAndReacquire(t *testing.T) {
	f := newLifecycleFixture(t)
	f.factory.onBuild = func(_ context.Context, d *lifecycleDriver) {
		if d.factory.builds() == 0 {
			d.bundleID, d.runnerID = "com.example.app", "runner-1"
		}
	}
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)

	if _, err := f.devices.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}

	second := f.factory.driver(1)
	if second.restoredBundle != "com.example.app" || second.restoredRunner != "runner-1" {
		t.Fatalf("restore offered = %q %q, want the first driver's launched app and runner", second.restoredBundle, second.restoredRunner)
	}
}

func TestDeviceSession_CheckHealthTakesAReleasedDeviceAgainAndReportsAHeldOne(t *testing.T) {
	f := newLifecycleFixture(t)
	if report := f.devices.CheckHealth(context.Background()); report.Selected {
		t.Fatalf("report = %+v, want not selected before any select_device", report)
	}
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)

	if report := f.devices.CheckHealth(context.Background()); !report.Selected || !report.Healthy {
		t.Fatalf("report = %+v, want the released device taken again and healthy", report)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)
	cli := f.newDevices("atomyx run", newLifecycleFactory())
	t.Cleanup(cli.Close)
	if err := f.selectDevice(cli); err != nil {
		t.Fatal(err)
	}

	report := f.devices.CheckHealth(context.Background())

	if !report.Selected || report.Healthy || report.Code != diag.CodeDeviceReleasedHeld {
		t.Fatalf("report = %+v, want selected, unhealthy, DEVICE_RELEASED_HELD", report)
	}
}

func TestSelectDeviceTool_ADeviceAnotherProcessHoldsReportsDeviceHeld(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
		Session: session.Deps{Leases: f.leases, Clock: clock.SystemClock{}, Command: "atomyx-mcp", Role: "mcp"},
	})

	result, decoded := callTool(t, cs, ctx, "select_device", map[string]any{"platform": "android", "id": "A"})

	if decoded["ok"] != false || decoded["code"] != diag.CodeDeviceHeld {
		t.Fatalf("select_device = isError %v %v, want ok false with code DEVICE_HELD", result.IsError, decoded)
	}
	if reason, _ := decoded["reason"].(string); !strings.Contains(reason, "atomyx-mcp") {
		t.Fatalf("reason = %q, want it to name the holder", reason)
	}
}
