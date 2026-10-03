package session_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/wire"
)

// recordingDriver wraps the in-memory driver with the launched-app
// accessors the iOS drivers have, and records its lifecycle.
type recordingDriver struct {
	*mockdriver.Driver
	events *eventLog

	bundleID string
	runnerID string

	restoredBundle string
	restoredRunner string

	// onHierarchy runs inside every hierarchy capture before the inner
	// driver answers, so a test can act on the device mid-capture.
	onHierarchy func(context.Context)
}

func (d *recordingDriver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	if d.onHierarchy != nil {
		d.onHierarchy(ctx)
	}
	return d.Driver.Hierarchy(ctx)
}

func (d *recordingDriver) Disconnect() error {
	d.events.add("disconnect")
	return d.Driver.Disconnect()
}

func (d *recordingDriver) LaunchedApp() (string, string) { return d.bundleID, d.runnerID }

func (d *recordingDriver) RestoreLaunchedApp(_ context.Context, bundleID, fromRunnerID string) error {
	d.restoredBundle, d.restoredRunner = bundleID, fromRunnerID
	return nil
}

type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

// recordingFactory builds recordingDrivers and records each build with
// the context it received.
type recordingFactory struct {
	events *eventLog
	err    error

	mu      sync.Mutex
	builds  int
	drivers []*recordingDriver
	ctxs    []context.Context
	script  func(*recordingDriver)
	lost    <-chan error
}

func (f *recordingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *recordingFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	f.mu.Lock()
	f.builds++
	f.ctxs = append(f.ctxs, ctx)
	f.mu.Unlock()
	if f.err != nil {
		return device.Built{}, f.err
	}
	drv := &recordingDriver{Driver: mockdriver.New(), events: f.events}
	if f.script != nil {
		f.script(drv)
	}
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	f.mu.Lock()
	f.drivers = append(f.drivers, drv)
	f.mu.Unlock()
	return device.Built{Driver: drv, Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}), Lost: f.lost}, nil
}

func (f *recordingFactory) buildCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.builds
}

type fixedResolver struct {
	byID map[string]device.Identity
	err  error
}

func (r fixedResolver) Resolve(_ context.Context, d device.Descriptor) (device.Identity, error) {
	if r.err != nil {
		return device.Identity{}, r.err
	}
	return r.byID[d.ID], nil
}

type fakeReaper struct {
	mu       sync.Mutex
	calls    []reapCall
	err      error
	onReap   func()
	outcomes device.ReapOutcome
}

type reapCall struct{ path, udid string }

func (r *fakeReaper) Reap(_ context.Context, path, udid string) (device.ReapOutcome, error) {
	r.mu.Lock()
	r.calls = append(r.calls, reapCall{path: path, udid: udid})
	r.mu.Unlock()
	if r.onReap != nil {
		r.onReap()
	}
	return r.outcomes, r.err
}

func (r *fakeReaper) recorded() []reapCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]reapCall(nil), r.calls...)
}

func newManager(t *testing.T, clk clock.Clock) *lease.Manager {
	t.Helper()
	root := t.TempDir()
	m, err := lease.NewManager(lease.Environment{
		GOOS:         "linux",
		Getenv:       func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": root}[k] },
		UserCacheDir: func() (string, error) { return root, nil },
		Getuid:       os.Getuid,
		IsLocalFS:    func(string) (bool, error) { return true, nil },
		FallbackRoot: root,
		// A long temp path would otherwise force the fallback directory.
		MaxSocketPath: 4096,
	}, clk)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func androidDesc(id string) device.Descriptor {
	return device.Descriptor{ID: id, Platform: device.PlatformAndroid, Kind: device.KindDevice, State: device.StateOnline}
}

func iosDesc(id string) device.Descriptor {
	return device.Descriptor{ID: id, Platform: device.PlatformIOS, Kind: device.KindSimulator, State: device.StateOnline}
}

func baseDeps(t *testing.T, f device.WrappingFactory, m *lease.Manager) session.Deps {
	t.Helper()
	return session.Deps{Factory: f, Leases: m, Command: "atomyx run", Role: "cli", PID: os.Getpid()}
}

func codeOf(err error) string {
	code, _, _ := diag.Describe(err)
	return code
}

func TestOpen_BuildsTheDriverUnderTheLeaseAndCloseReleasesIt(t *testing.T) {
	events := &eventLog{}
	factory := &recordingFactory{events: events}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)

	s, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	key := lease.KeyFor("android", "A")
	status, err := m.Probe(context.Background(), key)
	if err != nil || !status.Held {
		t.Fatalf("Probe() = %+v, %v, want the device held while the session is open", status, err)
	}
	if status.Holder.Command != "atomyx run" || status.Holder.Role != "cli" {
		t.Fatalf("holder = %+v, want the opener's command and role", status.Holder)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := events.snapshot(); len(got) != 1 || got[0] != "disconnect" {
		t.Fatalf("events = %v, want one disconnect", got)
	}
	status, _ = m.Probe(context.Background(), key)
	if status.Held {
		t.Fatal("the lease is still held after Close")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := events.snapshot(); len(got) != 1 {
		t.Fatalf("events = %v, want Close to disconnect once", got)
	}
}

func TestOpen_ASecondOpenOfTheSameDeviceGetsDeviceHeldAndBuildsNothing(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)
	first, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()

	second := deps
	second.Command = "atomyx doctor"
	_, err = session.Open(context.Background(), second, session.Request{Descriptor: androidDesc("A")})

	code, message, remedy := diag.Describe(err)
	if code != diag.CodeDeviceHeld {
		t.Fatalf("code = %s (%v), want DEVICE_HELD", code, err)
	}
	if !strings.Contains(message, "atomyx run") || !strings.Contains(remedy, "Stop that process") {
		t.Fatalf("message %q remedy %q, want the holder's command and the operator remedy", message, remedy)
	}
	if factory.buildCount() != 1 {
		t.Fatalf("builds = %d, want the second open to build nothing", factory.buildCount())
	}
}

func TestOpen_AReacquireThatFindsTheDeviceHeldReportsDeviceReleasedHeld(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)
	holder, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()

	agent := deps
	agent.Command = "atomyx-mcp"
	_, err = session.Open(context.Background(), agent, session.Request{Descriptor: androidDesc("A"), Reacquire: true})

	code, message, remedy := diag.Describe(err)
	if code != diag.CodeDeviceReleasedHeld {
		t.Fatalf("code = %s (%v), want DEVICE_RELEASED_HELD", code, err)
	}
	if !strings.Contains(message, "atomyx run") || !strings.Contains(remedy, "ask the operator") {
		t.Fatalf("message %q remedy %q, want the holder's command and the agent remedy", message, remedy)
	}
}

func TestOpen_OneLeasePerHardwareIdentityEvenWhenTheHostIdsDiffer(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)
	deps.Identity = fixedResolver{byID: map[string]device.Identity{
		"R58N90":           {Platform: device.PlatformAndroid, HardwareID: "R58N90ABCDE"},
		"192.168.1.9:5555": {Platform: device.PlatformAndroid, HardwareID: "R58N90ABCDE"},
	}}
	usb, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("R58N90")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = usb.Close() }()

	_, err = session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("192.168.1.9:5555")})

	if codeOf(err) != diag.CodeDeviceHeld {
		t.Fatalf("code = %s (%v), want DEVICE_HELD for the same phone over Wi-Fi", codeOf(err), err)
	}
}

func TestOpen_DifferentDevicesDoNotContend(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)
	a, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	b, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("B")})
	if err != nil {
		t.Fatalf("Open(B) error = %v, want a different device to open", err)
	}
	_ = b.Close()
}

func TestOpen_IdentityFailuresAreCodedAndTakeNoLease(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		code    string
		remedy  string
		message string
	}{
		{"unauthorized", &device.DeviceStateError{ID: "A", State: device.StateUnauthorized}, diag.CodeAgentUnreachable, "authorization", "unauthorized"},
		{"offline", &device.DeviceStateError{ID: "A", State: device.StateOffline}, diag.CodeAgentUnreachable, "Reconnect", "offline"},
		{"no identity", &device.IdentityUnresolvedError{ID: "A"}, diag.CodeDeviceNotFound, "booting", "no usable hardware identity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			factory := &recordingFactory{events: &eventLog{}}
			m := newManager(t, clock.SystemClock{})
			deps := baseDeps(t, factory, m)
			deps.Identity = fixedResolver{err: tc.err}

			_, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})

			code, message, remedy := diag.Describe(err)
			if code != tc.code || !strings.Contains(message, tc.message) || !strings.Contains(remedy, tc.remedy) {
				t.Fatalf("Describe() = %s %q %q, want %s mentioning %q and %q", code, message, remedy, tc.code, tc.message, tc.remedy)
			}
			if factory.buildCount() != 0 {
				t.Fatal("a driver was built for a device whose identity could not be read")
			}
			if _, statErr := os.Stat(m.LockPath(lease.KeyFor("android", "A"))); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("lock file stat = %v, want no lease file for an unreadable identity", statErr)
			}
		})
	}
}

func TestOpen_ABuildFailureReleasesTheLease(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}, err: errors.New("boom")}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)

	if _, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")}); err == nil {
		t.Fatal("Open() error = nil, want the build failure")
	}

	status, _ := m.Probe(context.Background(), lease.KeyFor("android", "A"))
	if status.Held {
		t.Fatal("a failed build left the lease held")
	}
}

func TestOpen_TheBuildContextCarriesTheDeviceTag(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)
	deps.Identity = fixedResolver{byID: map[string]device.Identity{"A": {Platform: device.PlatformAndroid, HardwareID: "HW-A"}}}

	s, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	tag, ok := logging.DeviceFromContext(factory.ctxs[0])
	if !ok || tag.DeviceID != "A" || tag.HardwareID != "HW-A" {
		t.Fatalf("device tag = %+v (%v), want deviceId A and hardwareId HW-A", tag, ok)
	}
}

func TestOpen_ReapsAnOrphanedIosRunnerOnlyAfterTheLeaseIsHeld(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	var heldDuringReap bool
	reaper := &fakeReaper{outcomes: device.ReapOutcome{Found: true, Signaled: true, PGID: 4242}}
	reaper.onReap = func() {
		status, _ := m.Probe(context.Background(), lease.KeyFor("ios", "SIM-1"))
		heldDuringReap = status.Held
	}
	deps := baseDeps(t, factory, m)
	deps.Reaper = reaper

	s, err := session.Open(context.Background(), deps, session.Request{Descriptor: iosDesc("SIM-1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	calls := reaper.recorded()
	wantPath := device.RunnerStatePath(m.Dir(), string(lease.KeyFor("ios", "SIM-1")))
	if len(calls) != 1 || calls[0].path != wantPath || calls[0].udid != "SIM-1" {
		t.Fatalf("reap calls = %+v, want one call for %s and SIM-1", calls, wantPath)
	}
	if !heldDuringReap {
		t.Fatal("the reaper ran before the lease was held")
	}
}

func TestOpen_DoesNotReapWhenTheLeaseIsHeldElsewhere(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	reaper := &fakeReaper{}
	deps := baseDeps(t, factory, m)
	deps.Reaper = reaper
	holder, err := session.Open(context.Background(), deps, session.Request{Descriptor: iosDesc("SIM-1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	before := len(reaper.recorded())

	if _, err := session.Open(context.Background(), deps, session.Request{Descriptor: iosDesc("SIM-1")}); codeOf(err) != diag.CodeDeviceHeld {
		t.Fatalf("code = %s, want DEVICE_HELD", codeOf(err))
	}

	if got := len(reaper.recorded()); got != before {
		t.Fatalf("reap calls = %d, want none from an open that never got the lease", got-before)
	}
}

func TestOpen_AndroidNeverReapsAndAReapFailureDoesNotBlockTheOpen(t *testing.T) {
	m := newManager(t, clock.SystemClock{})
	reaper := &fakeReaper{err: errors.New("still answering")}
	deps := baseDeps(t, &recordingFactory{events: &eventLog{}}, m)
	deps.Reaper = reaper

	a, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	_ = a.Close()
	if got := len(reaper.recorded()); got != 0 {
		t.Fatalf("reap calls for Android = %d, want 0", got)
	}

	ios, err := session.Open(context.Background(), deps, session.Request{Descriptor: iosDesc("SIM-1")})
	if err != nil {
		t.Fatalf("Open() error = %v, want a reap failure logged but not fatal", err)
	}
	_ = ios.Close()
}

func TestOpen_RestoresTheLaunchedAppIntoTheRebuiltDriver(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)

	s, err := session.Open(context.Background(), deps, session.Request{
		Descriptor: iosDesc("SIM-1"),
		Restore:    &session.LaunchState{BundleID: "com.example.app", RunnerID: "runner-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	drv := factory.drivers[0]
	if drv.restoredBundle != "com.example.app" || drv.restoredRunner != "runner-1" {
		t.Fatalf("restore = %q %q, want the requested state offered to the new driver", drv.restoredBundle, drv.restoredRunner)
	}
}

func TestSession_CloseCapturesTheLaunchedAppBeforeTheDriverDisconnects(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	factory.script = func(d *recordingDriver) { d.bundleID, d.runnerID = "com.example.app", "runner-1" }
	m := newManager(t, clock.SystemClock{})
	s, err := session.Open(context.Background(), baseDeps(t, factory, m), session.Request{Descriptor: iosDesc("SIM-1")})
	if err != nil {
		t.Fatal(err)
	}
	factory.drivers[0].bundleID = "com.example.after"

	_ = s.Close()
	factory.drivers[0].bundleID = "cleared-by-disconnect"

	if got := s.LaunchState(); got != (session.LaunchState{BundleID: "com.example.after", RunnerID: "runner-1"}) {
		t.Fatalf("LaunchState() = %+v, want the state captured at Close", got)
	}
}

func TestSession_HolderMetadataFollowsTheSessionState(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)
	s, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	describe := func() string {
		_, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
		_, message, _ := diag.Describe(err)
		return message
	}

	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	if got := describe(); !strings.Contains(got, "idle") {
		t.Fatalf("held message %q, want the holder reported idle", got)
	}
	if err := s.SetRunning("run-7"); err != nil {
		t.Fatal(err)
	}
	if got := describe(); !strings.Contains(got, "running (run open)") {
		t.Fatalf("held message %q, want the holder reported running with a run open", got)
	}
	if err := s.SetRunning(""); err != nil {
		t.Fatal(err)
	}
	if got := describe(); !strings.Contains(got, "running") || strings.Contains(got, "run open") {
		t.Fatalf("held message %q, want plain running", got)
	}
}

func TestSession_AReplacedLockFileThatCannotBeReacquiredMovesTheSessionToLost(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	manual := clock.NewManualClock(0)
	m := newManager(t, manual)
	s, err := session.Open(context.Background(), baseDeps(t, factory, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	select {
	case <-s.Lost():
		t.Fatal("the session reports lost before anything happened")
	default:
	}

	// Another process replaces the lock file and holds the new one.
	lockPath := m.LockPath(lease.KeyFor("android", "A"))
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
	defer func() { _ = intruder.Close() }()
	if err := lockExclusive(intruder); err != nil {
		t.Fatal(err)
	}

	manual.AwaitSleepers(1)
	manual.Advance(lease.InodeRecheckPeriod.Milliseconds())
	for range int(lease.AcquireRetryWindow / lease.AcquirePollInterval) {
		manual.AwaitSleepers(1)
		manual.Advance(lease.AcquirePollInterval.Milliseconds())
	}
	<-s.Lost()

	if err := s.LostErr(); !errors.Is(err, lease.ErrLost) || !errors.Is(err, session.ErrDeviceLost) {
		t.Fatalf("LostErr() = %v, want lease.ErrLost wrapped in session.ErrDeviceLost", err)
	}
}

func TestSession_AProcessExitReportedByTheFactoryMovesTheSessionToLost(t *testing.T) {
	events := &eventLog{}
	lost := make(chan error, 1)
	factory := &recordingFactory{events: events, lost: lost}
	m := newManager(t, clock.SystemClock{})
	s, err := session.Open(context.Background(), baseDeps(t, factory, m), session.Request{Descriptor: iosDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Lost():
		t.Fatal("the session reports lost before the runner exited")
	default:
	}

	lost <- &device.ProcessExitError{Component: device.LostRunner}
	<-s.Lost()

	err = s.LostErr()
	var exit *device.ProcessExitError
	if !errors.Is(err, session.ErrDeviceLost) || !errors.As(err, &exit) || exit.Component != device.LostRunner {
		t.Fatalf("LostErr() = %v, want session.ErrDeviceLost wrapping the runner exit", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := events.snapshot(); len(got) != 1 || got[0] != "disconnect" {
		t.Fatalf("events = %v, want the driver disconnected once", got)
	}
	status, _ := m.Probe(context.Background(), lease.KeyFor("ios", "A"))
	if status.Held {
		t.Fatal("the lease is still held after a lost session closed")
	}
}

func TestSession_ClosingBeforeAnyLossLeavesTheRunnerWatcherStopped(t *testing.T) {
	lost := make(chan error, 1)
	factory := &recordingFactory{events: &eventLog{}, lost: lost}
	s, err := session.Open(context.Background(), baseDeps(t, factory, newManager(t, clock.SystemClock{})), session.Request{Descriptor: iosDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	lost <- &device.ProcessExitError{Component: device.LostRunner}
	select {
	case <-s.Lost():
		t.Fatal("a closed session reported a loss")
	default:
	}
}

func TestOpen_BuildsEveryDriverInsideAnObservingDecorator(t *testing.T) {
	s, err := session.Open(context.Background(), session.Deps{Factory: device.NewMockDriverFactory()}, session.Request{Descriptor: device.Descriptor{ID: "d1", Platform: device.PlatformAndroid}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	observed, ok := s.Built().Driver.(session.ObservedDriver)
	if !ok {
		t.Fatalf("Built().Driver is %T, want an observing decorator", s.Built().Driver)
	}
	before := observed.Epoch()
	if err := s.Orchestra().TapAt(context.Background(), wire.Point{X: 3, Y: 4}); err != nil {
		t.Fatalf("TapAt() error = %v", err)
	}
	if observed.Epoch() == before {
		t.Fatal("an Orchestra action did not advance the epoch, so the Orchestra bypasses the decorator")
	}
}

func TestOpen_RecordsTheStartingStateInTheHolderMetadata(t *testing.T) {
	factory := &recordingFactory{events: &eventLog{}}
	m := newManager(t, clock.SystemClock{})
	deps := baseDeps(t, factory, m)
	s, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	_, err = session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	_, message, _ := diag.Describe(err)

	if !strings.Contains(message, "running") || strings.Contains(message, "idle") {
		t.Fatalf("held message %q, want the holder reported running as soon as it opened", message)
	}
}
