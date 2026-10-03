package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
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

func (d *lifecycleDriver) RestoreLaunchedApp(_ context.Context, bundleID, fromRunnerID string) error {
	d.restoredBundle, d.restoredRunner = bundleID, fromRunnerID
	return nil
}

// lifecycleFactory builds lifecycleDrivers and records them.
type lifecycleFactory struct {
	disconnected chan struct{}
	onBuild      func(ctx context.Context, d *lifecycleDriver)
	// lost, when set, is the loss signal every built driver reports
	// through.
	lost chan error
	// runnerStops counts the Release calls of every built device, which stop
	// a provisioned runner.
	runnerStops atomic.Int32

	mu      sync.Mutex
	drivers []*lifecycleDriver
}

func newLifecycleFactory() *lifecycleFactory {
	return &lifecycleFactory{disconnected: make(chan struct{}, 32)}
}

func (f *lifecycleFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
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
	var lost <-chan error
	if f.lost != nil {
		lost = f.lost
	}
	return device.Built{Driver: drv, Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}), Lost: lost, Release: func() { f.runnerStops.Add(1) }}, nil
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

type lifecycleFixture struct {
	t       *testing.T
	clk     *clock.ManualClock
	leases  *sessiontest.Leases
	factory *lifecycleFactory
	root    string
	runs    *runSession
	devices *DeviceSession
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	leases, err := sessiontest.NewLeases(shortTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	f := &lifecycleFixture{
		t: t, clk: clock.NewManualClock(0), leases: leases,
		factory: newLifecycleFactory(), root: t.TempDir(),
	}
	runStore := storage.NewRunStore()
	f.runs = newRunSession(runSessionDeps{
		Store:                   runStore,
		Catalog:                 testRunCatalog(storage.NewInMemoryStorage(), f.root),
		Starter:                 testRunStarter(f.root),
		Clock:                   f.clk,
		Logger:                  logging.NoopLogger{},
		SetInterventionRecorder: func(r session.InterventionRecorder) { f.devices.SetInterventionRecorder(r) },
	})
	f.devices = f.newDevices("atomyx-mcp", f.factory)
	t.Cleanup(f.devices.Close)
	return f
}

// newDevices builds another DeviceSession over the same lease directory,
// standing in for another Atomyx process.
func (f *lifecycleFixture) newDevices(command string, factory device.WrappingFactory) *DeviceSession {
	return NewDeviceSession(DeviceSessionDeps{
		Factory: factory, Probe: device.NewStaticProbe(), Clock: f.clk, TimerClock: f.clk, Logger: logging.NoopLogger{},
		Session:  session.Deps{Leases: f.leases, Sockets: f.leases, Clock: clock.SystemClock{}, Command: command},
		Timeouts: session.Timeouts{McpIdleMs: testIdleMs, OpenRunIdleMs: testOpenRunMs, DeviceAcquireMs: 120_000},
		Runs:     f.runs,
	})
}

// newDevicesPreparing is newDevices with a runner preparer, timed on the
// fixture clock so a test decides when the build budget elapses.
func (f *lifecycleFixture) newDevicesPreparing(command string, factory device.WrappingFactory, preparer device.RunnerPreparer) *DeviceSession {
	return NewDeviceSession(DeviceSessionDeps{
		Factory: factory, Probe: device.NewStaticProbe(), Clock: f.clk, TimerClock: f.clk, Logger: logging.NoopLogger{},
		Session:  session.Deps{Leases: f.leases, Sockets: f.leases, Clock: f.clk, Command: command, Preparer: preparer},
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
	held, err := f.leases.Held(context.Background(), "android", "A")
	if err != nil {
		f.t.Fatal(err)
	}
	return held
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

	_ = cli.Disconnect(context.Background())
	if _, err := f.devices.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v after the other process let go", err)
	}
}

func TestDeviceSession_AnExplicitDisconnectForgetsTheDevice(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}

	_ = f.devices.Disconnect(context.Background())
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
	f.startRun()
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

	if f.runs.OpenRunID() != "" {
		t.Fatal("the open run is still open after the open-run timeout")
	}
	meta := f.onlyRun()
	if meta.Outcome != runfolder.OutcomeCancelled || meta.Result != runfolder.ResultCancelled {
		t.Fatalf("closed run = %s/%s, want result cancelled with outcome cancelled", meta.Result, meta.Outcome)
	}
	if runfolder.NewLockLiveness(f.root).IsLive(meta.RunID) {
		t.Fatal("the closed run still holds its lock")
	}
}

// startRun opens a run the way start_run does, with no device named.
func (f *lifecycleFixture) startRun() startedRun {
	f.t.Helper()
	started, err := f.runs.Start(context.Background(), "smoke", nil, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	return started
}

// onlyRun returns the meta of the one run folder under the root.
func (f *lifecycleFixture) onlyRun() runfolder.Meta {
	f.t.Helper()
	metas, _, err := runfolder.NewReader(f.root).List()
	if err != nil || len(metas) != 1 {
		f.t.Fatalf("run folders = %d (%v), want exactly one", len(metas), err)
	}
	return metas[0]
}

func TestDeviceSession_EveryCallOnTheOpenRunRestartsTheOpenRunTimeout(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.startRun()
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
	started := f.startRun()
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
	f.startRun()

	lockPath := f.leases.LockPathFor("android", "A")
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

	f.leases.FireRecheck()
	f.awaitRelease(f.devices)

	if got := f.state(f.devices); got != holderLost || f.devices.Current() != nil {
		t.Fatalf("state = %s current = %v, want lost with no device bound", got, f.devices.Current())
	}
	if f.runs.OpenRunID() != "" {
		t.Fatal("the run in flight stayed open after the device was lost")
	}
	meta := f.onlyRun()
	if meta.Outcome != runfolder.OutcomeDeviceLost || meta.Result != runfolder.ResultCancelled {
		t.Fatalf("lost-device run = %s/%s, want result cancelled with outcome deviceLost", meta.Result, meta.Outcome)
	}
	if runfolder.NewLockLiveness(f.root).IsLive(meta.RunID) {
		t.Fatal("the lost-device run still holds its lock")
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

func TestDeviceSession_CheckHealthReportsTheStateWithoutTouchingTheDevice(t *testing.T) {
	f := newLifecycleFixture(t)
	if report := f.devices.CheckHealth(context.Background()); report.Selected || report.State != "unselected" {
		t.Fatalf("report = %+v, want not selected with state unselected before any select_device", report)
	}
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	if report := f.devices.CheckHealth(context.Background()); !report.Selected || !report.Healthy || report.State != "idle" {
		t.Fatalf("report = %+v, want a healthy idle device", report)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)

	report := f.devices.CheckHealth(context.Background())

	if !report.Selected || report.Healthy || report.State != "released" || report.Code != "" {
		t.Fatalf("report = %+v, want selected, state released, not probed", report)
	}
	if got := f.factory.builds(); got != 1 {
		t.Fatalf("builds = %d, want 1 because check_health must not take the device again", got)
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
		Session: session.Deps{Leases: f.leases, Sockets: f.leases, Clock: clock.SystemClock{}, Command: "atomyx-mcp", Role: "mcp"},
	})

	result, decoded := callTool(t, cs, ctx, "select_device", map[string]any{"platform": "android", "id": "A"})

	if decoded["ok"] != false || decoded["code"] != diag.CodeDeviceHeld {
		t.Fatalf("select_device = isError %v %v, want ok false with code DEVICE_HELD", result.IsError, decoded)
	}
	if reason, _ := decoded["reason"].(string); !strings.Contains(reason, "atomyx-mcp") {
		t.Fatalf("reason = %q, want it to name the holder", reason)
	}
}

func TestDeviceSession_CloseFinalizesTheOpenRunAsCancelled(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.startRun()

	f.devices.Close()

	if f.runs.OpenRunID() != "" {
		t.Fatal("Close left the run open")
	}
	meta := f.onlyRun()
	if meta.Outcome != runfolder.OutcomeCancelled || meta.CompletedAt == nil {
		t.Fatalf("closed run = %+v, want a cancelled run", meta)
	}
	if runfolder.NewLockLiveness(f.root).IsLive(meta.RunID) {
		t.Fatal("the closed run still holds its lock")
	}
}

func TestDeviceSession_CheckHealthReportsBorrowedWithoutProbing(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.devices.mu.Lock()
	f.devices.sess, f.devices.active, f.devices.state = nil, nil, holderBorrowed
	f.devices.mu.Unlock()

	report := f.devices.CheckHealth(context.Background())

	if !report.Selected || report.Healthy || report.State != "borrowed" || report.Remedy == "" {
		t.Fatalf("CheckHealth() = %+v, want a selected, unprobed, borrowed report with a remedy", report)
	}
}
