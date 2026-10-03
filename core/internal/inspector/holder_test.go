package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

const testIdleMs = 600_000

// fakeHeld is a held device whose Close is observable.
type fakeHeld struct {
	*stubController
	device *fakeDevice
	closed chan struct{}
	lost   chan struct{}
	once   sync.Once
	// closes counts every Close call, including repeated ones.
	closes    atomic.Int32
	borrowers chan string
}

var errFakeLost = errors.New("the control plane went away")

func (h *fakeHeld) Lost() <-chan struct{} { return h.lost }

func (h *fakeHeld) LostErr() error {
	select {
	case <-h.lost:
		return errFakeLost
	default:
		return nil
	}
}

func (h *fakeHeld) RecordIntervention(kind, client string) {
	h.borrowers <- kind + ":" + client
}

func (h *fakeHeld) Close() error {
	h.closes.Add(1)
	h.once.Do(func() {
		h.device.release()
		close(h.closed)
	})
	return nil
}

// fakeDevice models the device lease: opening it while it is held
// fails with the device-held code, the way a second process does.
type fakeDevice struct {
	mu    sync.Mutex
	held  bool
	opens int
}

func (d *fakeDevice) open(context.Context) (Target, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.held {
		return nil, diag.New(diag.CodeDeviceHeld, "another process holds the device", "Stop that process, then try again.", nil)
	}
	d.held = true
	d.opens++
	ctrl := &stubController{snap: sampleSnapshot(), freshSnap: sampleSnapshot()}
	ctrl.info.State = session.StateIdle
	return &fakeHeld{stubController: ctrl, device: d, closed: make(chan struct{}), lost: make(chan struct{}), borrowers: make(chan string, 4)}, nil
}

func (d *fakeDevice) release() {
	d.mu.Lock()
	d.held = false
	d.mu.Unlock()
}

type hostHarness struct {
	*apiHarness
	host  *Host
	clk   *clock.ManualClock
	dev   *fakeDevice
	first *fakeHeld
}

func newHostHarness(t *testing.T) *hostHarness {
	t.Helper()
	return newHostHarnessFollowing(t, nil)
}

func newHostHarnessFollowing(t *testing.T, follow Opener) *hostHarness {
	t.Helper()
	dev := &fakeDevice{}
	opened, err := dev.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first := opened.(*fakeHeld)
	clk := clock.NewManualClock(1_000)
	host, err := NewHost(HostConfig{Target: first, Open: dev.open, Follow: follow, Clock: clk, IdleMs: testIdleMs, Logger: newRecordingLogger()})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{Source: host, Logger: newRecordingLogger(), Entropy: &counterReader{}, Groups: []RouteGroup{host.Routes(ReadRoutes, SelectorRoutes)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = srv.Close()
		_ = host.Close()
	})
	clk.AwaitSleepers(1)
	return &hostHarness{
		apiHarness: &apiHarness{srv: srv, ctrl: first.stubController, token: authorize(t, srv)},
		host:       host, clk: clk, dev: dev, first: first,
	}
}

func (h *hostHarness) released() bool {
	select {
	case <-h.first.closed:
		return true
	default:
		return false
	}
}

func TestHost_PollsDoNotResetTheIdleTimer(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs - 1)
	for _, path := range []string{routeSession, routeTree, routeScreenshot} {
		if rec := h.get(path); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, body %s", path, rec.Code, rec.Body)
		}
	}
	h.clk.Advance(1)
	<-h.first.closed
}

func TestHost_AnOperatorActionResetsTheIdleTimer(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs - 1)
	if rec := h.post(routeTreeFresh, ""); rec.Code != http.StatusOK {
		t.Fatalf("fresh = %d, body %s", rec.Code, rec.Body)
	}
	h.clk.Advance(1)
	h.clk.AwaitSleepers(1)
	if h.released() {
		t.Fatal("the device was released although an operator action ran one millisecond ago")
	}
	h.clk.Advance(testIdleMs - 1)
	<-h.first.closed
}

func TestHost_EveryOperatorRouteCountsAsActivity(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs - 1)
	h.post(routeSelectorTest, `{"selector":{"label":"Sign in"}}`)
	h.clk.Advance(1)
	h.clk.AwaitSleepers(1)
	if h.released() {
		t.Fatal("a selector test did not count as activity")
	}
}

func TestHost_AReleasedDeviceAnswersReadsWithACodedBodyAndReportsTheState(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs)
	<-h.first.closed
	for _, path := range []string{routeTree, routeScreenshot, routeEvents} {
		rec := h.get(path)
		if rec.Code != http.StatusGone || codeOf(t, rec) != codeDeviceReleased {
			t.Errorf("GET %s = %d %s, want 410 %s", path, rec.Code, rec.Body, codeDeviceReleased)
		}
	}
	rec := h.get(routeSession)
	var info struct{ State string }
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || rec.Code != http.StatusOK || info.State != string(session.StateReleased) {
		t.Fatalf("session = %d %s, want 200 with state released", rec.Code, rec.Body)
	}
	if rec := h.post(routeTreeFresh, ""); rec.Code != http.StatusGone || codeOf(t, rec) != codeDeviceReleased {
		t.Errorf("fresh = %d %s, want 410 %s", rec.Code, rec.Body, codeDeviceReleased)
	}
}

func TestHost_ASecondProcessIsRefusedBeforeTheReleaseAndAdmittedAfter(t *testing.T) {
	h := newHostHarness(t)
	_, err := h.dev.open(context.Background())
	if err == nil {
		t.Fatal("a second process opened a held device")
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeDeviceHeld {
		t.Fatalf("code = %q, want %s", code, diag.CodeDeviceHeld)
	}
	h.clk.Advance(testIdleMs)
	<-h.first.closed
	other, err := h.dev.open(context.Background())
	if err != nil {
		t.Fatalf("open after the release: %v", err)
	}
	_ = other.Close()
}

func TestHost_ReconnectReacquiresAndSwapsTheControllerWithoutANewToken(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs)
	<-h.first.closed

	rec := h.post(routeReconnect, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reconnect = %d, body %s", rec.Code, rec.Body)
	}
	if h.dev.opens != 2 {
		t.Fatalf("opens = %d, want 2", h.dev.opens)
	}
	if rec := h.get(routeTree); rec.Code != http.StatusOK {
		t.Fatalf("tree after reconnect = %d, body %s", rec.Code, rec.Body)
	}
	if h.ctrl.latestRead != 0 {
		t.Fatalf("the released controller was read %d times after the release", h.ctrl.latestRead)
	}
	cur, _ := h.host.held()
	if cur == Target(h.first) {
		t.Fatal("the routes still read the first controller")
	}
	h.clk.AwaitSleepers(1)
	h.clk.Advance(testIdleMs)
	<-cur.(*fakeHeld).closed
}

func TestHost_ReconnectReportsAHeldDeviceUnchangedAndStaysReleased(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs)
	<-h.first.closed
	other, err := h.dev.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rec := h.post(routeReconnect, "")
	if rec.Code != http.StatusConflict || codeOf(t, rec) != diag.CodeDeviceHeld {
		t.Fatalf("reconnect = %d %s, want 409 %s", rec.Code, rec.Body, diag.CodeDeviceHeld)
	}
	if rec := h.get(routeTree); rec.Code != http.StatusGone {
		t.Fatalf("tree = %d, want 410 while still released", rec.Code)
	}
	_ = other.Close()
	if rec := h.post(routeReconnect, ""); rec.Code != http.StatusOK {
		t.Fatalf("reconnect after the other process left = %d, body %s", rec.Code, rec.Body)
	}
}

func TestHost_ReconnectOnAHeldDeviceOnlyCountsAsActivity(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs - 1)
	if rec := h.post(routeReconnect, ""); rec.Code != http.StatusOK {
		t.Fatalf("reconnect = %d, body %s", rec.Code, rec.Body)
	}
	if h.dev.opens != 1 {
		t.Fatalf("opens = %d, want the held device kept", h.dev.opens)
	}
	h.clk.Advance(1)
	h.clk.AwaitSleepers(1)
	if h.released() {
		t.Fatal("reconnect did not count as activity")
	}
}

func TestHost_CloseReleasesTheDeviceOnceAndStopsTheTimer(t *testing.T) {
	h := newHostHarness(t)
	if err := h.host.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.host.Close(); err != nil {
		t.Fatal(err)
	}
	<-h.first.closed
	h.clk.AwaitIdle()
	if _, err := h.host.FreshTree(context.Background()); !errors.Is(err, ErrReleased) {
		t.Fatalf("FreshTree after Close = %v, want ErrReleased", err)
	}
}

func TestNewHost_RequiresItsDependencies(t *testing.T) {
	if _, err := NewHost(HostConfig{}); err == nil {
		t.Fatal("an empty config built a host")
	}
}

func TestHost_TheHolderIsIdleAfterOpenAndRunningOnlyDuringAFreshCapture(t *testing.T) {
	h := newHostHarness(t)
	if got := h.first.holderState(); got != "idle" {
		t.Fatalf("holder after open = %q, want idle", got)
	}
	var during string
	h.first.duringFresh = func() { during = h.first.holderState() }
	if rec := h.post(routeTreeFresh, ""); rec.Code != http.StatusOK {
		t.Fatalf("fresh = %d, body %s", rec.Code, rec.Body)
	}
	if during != "running" {
		t.Errorf("holder during the capture = %q, want running", during)
	}
	if got := h.first.holderState(); got != "idle" {
		t.Errorf("holder after the capture = %q, want idle", got)
	}
}

func TestHost_TheHolderIsIdleAfterAReconnect(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs)
	<-h.first.closed
	if err := h.host.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, ok := h.host.held()
	if !ok {
		t.Fatal("the device is not held after the reconnect")
	}
	if got := current.(*fakeHeld).holderState(); got != "idle" {
		t.Fatalf("holder after reconnect = %q, want idle", got)
	}
}

// lockProbeViewer is a viewer that reports whether the host's lock was
// held while the host read its Info.
type lockProbeViewer struct {
	*fakeViewer
	probe func()
}

func (v *lockProbeViewer) Info() session.Info {
	if v.probe != nil {
		v.probe()
	}
	return v.fakeViewer.Info()
}

func TestHost_NeverReadsATargetsInfoUnderItsLock(t *testing.T) {
	first := &lockProbeViewer{fakeViewer: newFakeViewer()}
	next := &lockProbeViewer{fakeViewer: newFakeViewer()}
	var host *Host
	var underLock atomic.Int32
	probe := func() {
		if host.mu.TryLock() {
			host.mu.Unlock()
			return
		}
		underLock.Add(1)
	}
	var err error
	host, err = NewHost(HostConfig{
		Target: first, Open: func(context.Context) (Target, error) { return next, nil },
		Clock: clock.NewManualClock(1_000), IdleMs: testIdleMs, Logger: newRecordingLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	first.probe, next.probe = probe, probe

	close(first.done)
	<-first.closed
	if err := host.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}

	if n := underLock.Load(); n != 0 {
		t.Fatalf("Info was read %d times while the host lock was held", n)
	}
}
