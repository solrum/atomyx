package session_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
)

// recordingClock never blocks and remembers every wait it was asked for.
type recordingClock struct {
	mu    sync.Mutex
	waits []int64
}

func (c *recordingClock) NowMs() int64 { return 0 }

func (c *recordingClock) Sleep(ctx context.Context, ms int64) error {
	c.mu.Lock()
	c.waits = append(c.waits, ms)
	c.mu.Unlock()
	return ctx.Err()
}

func (c *recordingClock) total() (sum int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.waits {
		sum += w
	}
	return sum
}

// scriptedProber answers the first probes from a script and then
// delegates to the real prober, or repeats the last scripted answer when
// there is none.
type scriptedProber struct {
	script []lease.Status
	inner  session.LeaseProber
	// onProbe, when set, is told the number of probes made so far.
	onProbe func(n int)

	mu    sync.Mutex
	calls int
}

func (p *scriptedProber) Probe(ctx context.Context, key lease.Key) (lease.Status, error) {
	p.mu.Lock()
	i := p.calls
	p.calls++
	p.mu.Unlock()
	if p.onProbe != nil {
		p.onProbe(i + 1)
	}
	if i < len(p.script) {
		return p.script[i], nil
	}
	if p.inner != nil {
		return p.inner.Probe(ctx, key)
	}
	return p.script[len(p.script)-1], nil
}

func (p *scriptedProber) probes() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func heldStatus(h lease.Holder) lease.Status {
	return lease.Status{Held: true, Holder: h, HolderKnown: true}
}

func countingDial(dials *int) func(context.Context, session.RemoteConfig, device.Descriptor) (*session.RemoteHandle, error) {
	return func(ctx context.Context, cfg session.RemoteConfig, desc device.Descriptor) (*session.RemoteHandle, error) {
		*dials++
		return session.DialRemote(ctx, cfg, desc)
	}
}

func openSocketHolder(t *testing.T) (*session.Session, *recordingFactory, *lease.Manager) {
	t.Helper()
	m := newSocketManager(t, clock.SystemClock{})
	factory := &recordingFactory{events: &eventLog{}}
	s, err := session.Open(context.Background(), socketDeps(t, factory, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, factory, m
}

func TestAttach_ConnectsToALiveHolderAndFollowsItsStepsAndCapturesMasked(t *testing.T) {
	s, factory, m := openSocketHolder(t)
	result, err := session.Attach(context.Background(), session.AttachDeps{Leases: m, Client: "inspect"}, androidDesc("A"))
	if err != nil || result.Handle == nil || result.Free {
		t.Fatalf("Attach() = %+v, %v, want a handle", result, err)
	}
	h := result.Handle
	defer func() { _ = h.Close() }()
	feed, err := h.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}

	s.PublishStep(stepLine(1))
	awaitObservation(t, feed, session.ObservedStep, 1)
	lines, _, err := h.StepsSince("", 0)
	if err != nil || len(lines) != 1 || lines[0].Seq != 1 {
		t.Fatalf("StepsSince() = %+v, %v, want step 1", lines, err)
	}
	factory.drivers[0].StageHierarchy(secureTree(typedPassword))
	if _, err := s.Orchestra().Hierarchy(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitObservation(t, feed, session.ObservedTree, s.Latest().TreeRevision)
	if text := h.Latest().Tree.Children[1].Attributes["text"]; text == typedPassword || text == "" {
		t.Fatalf("password text = %q, want the mask", text)
	}
	if info := h.Info(); info.Holder.Command != "atomyx run" || info.State != session.StateRunning {
		t.Fatalf("Info() = %+v, want the running holder", info)
	}
}

func TestAttach_AFreeDeviceReportsThatTheCallerMayAcquire(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	var dials int

	result, err := session.Attach(context.Background(), session.AttachDeps{Leases: m, Dial: countingDial(&dials)}, androidDesc("A"))

	if err != nil || !result.Free || result.Handle != nil || dials != 0 {
		t.Fatalf("Attach() = %+v, %v, dials %d, want Free and no connection", result, err, dials)
	}
}

func TestAttach_AHolderThatServesNoSocketIsReportedWithItsCommand(t *testing.T) {
	prober := &scriptedProber{script: []lease.Status{heldStatus(lease.Holder{PID: 7, Command: "atomyx doctor", SocketError: "path too long"})}}
	clk := &recordingClock{}
	var dials int

	_, err := session.Attach(context.Background(), session.AttachDeps{Leases: prober, Clock: clk, Dial: countingDial(&dials)}, androidDesc("A"))

	var noSocket *session.HolderServesNoSocketError
	if !errors.As(err, &noSocket) {
		t.Fatalf("Attach() error = %v, want *HolderServesNoSocketError", err)
	}
	code, message, _ := diag.Describe(err)
	if code != diag.CodeDeviceHeld || !strings.Contains(message, "atomyx doctor") {
		t.Fatalf("code %s message %q, want DEVICE_HELD naming the command", code, message)
	}
	if dials != 0 || len(clk.waits) != 0 || prober.probes() != 1 {
		t.Fatalf("dials %d waits %v probes %d, want one probe and no connection or wait", dials, clk.waits, prober.probes())
	}
}

func TestAttach_AStartingHolderIsWaitedForWithBoundedBackoffThenReported(t *testing.T) {
	prober := &scriptedProber{script: []lease.Status{heldStatus(lease.Holder{PID: 7, Command: "atomyx run"})}}
	clk := &recordingClock{}

	_, err := session.Attach(context.Background(), session.AttachDeps{Leases: prober, Clock: clk}, androidDesc("A"))

	var starting *session.HolderStartingError
	if !errors.As(err, &starting) || diagCodeOf(err) != diag.CodeDeviceHeld {
		t.Fatalf("Attach() error = %v, want *HolderStartingError with DEVICE_HELD", err)
	}
	if got := clk.total(); got != session.AttachStartingBudgetMs {
		t.Fatalf("waited %d ms in total, want exactly the starting budget %d", got, session.AttachStartingBudgetMs)
	}
	if clk.waits[0] != session.AttachBackoffInitialMs || clk.waits[1] != 2*session.AttachBackoffInitialMs {
		t.Fatalf("first waits = %v, want the initial wait then its double", clk.waits[:2])
	}
	for _, w := range clk.waits {
		if w > session.AttachBackoffMaxMs {
			t.Fatalf("wait %d ms passes the cap %d", w, session.AttachBackoffMaxMs)
		}
	}
	if prober.probes() != len(clk.waits)+1 {
		t.Fatalf("probes = %d, waits = %d, want one probe before each wait and a last one", prober.probes(), len(clk.waits))
	}
}

func diagCodeOf(err error) string {
	code, _, _ := diag.Describe(err)
	return code
}

func TestAttach_AHolderThatPublishesItsSocketWhileWaitingIsConnected(t *testing.T) {
	_, _, m := openSocketHolder(t)
	starting := heldStatus(lease.Holder{PID: 7, Command: "atomyx run"})
	prober := &scriptedProber{script: []lease.Status{starting, starting}, inner: m}
	clk := &recordingClock{}

	result, err := session.Attach(context.Background(), session.AttachDeps{Leases: prober, Clock: clk}, androidDesc("A"))

	if err != nil || result.Handle == nil {
		t.Fatalf("Attach() = %+v, %v, want a handle once the socket is published", result, err)
	}
	defer func() { _ = result.Handle.Close() }()
	if len(clk.waits) != 2 {
		t.Fatalf("waits = %v, want one wait per starting probe", clk.waits)
	}
}

func TestAttach_AHolderThatReleasesAfterAFailedConnectReportsTheDeviceFree(t *testing.T) {
	gone := heldStatus(lease.Holder{PID: 7, Command: "atomyx run", Socket: t.TempDir() + "/gone.sock"})
	prober := &scriptedProber{script: []lease.Status{gone, gone, {}}}
	clk := &recordingClock{}

	result, err := session.Attach(context.Background(), session.AttachDeps{Leases: prober, Clock: clk}, androidDesc("A"))

	if err != nil || !result.Free || result.Handle != nil {
		t.Fatalf("Attach() = %+v, %v, want Free", result, err)
	}
	if len(clk.waits) != 2 || clk.waits[0] != session.AttachBackoffInitialMs {
		t.Fatalf("waits = %v, want two backoff waits before the free probe", clk.waits)
	}
}

func TestAttach_AHolderThatKeepsTheLeaseButRefusesConnectionsIsReportedAfterTheReconnectBudget(t *testing.T) {
	gone := heldStatus(lease.Holder{PID: 7, Command: "atomyx run", Socket: t.TempDir() + "/gone.sock"})
	prober := &scriptedProber{script: []lease.Status{gone}}
	clk := &recordingClock{}

	_, err := session.Attach(context.Background(), session.AttachDeps{Leases: prober, Clock: clk}, androidDesc("A"))

	var unreachable *session.HolderUnreachableError
	if !errors.As(err, &unreachable) || diagCodeOf(err) != diag.CodeDeviceHeld {
		t.Fatalf("Attach() error = %v, want *HolderUnreachableError with DEVICE_HELD", err)
	}
	if got := clk.total(); got != session.AttachReconnectBudgetMs {
		t.Fatalf("waited %d ms in total, want the reconnect budget %d", got, session.AttachReconnectBudgetMs)
	}
}

func TestAttach_ACanceledContextEndsTheWaitWithItsOwnError(t *testing.T) {
	prober := &scriptedProber{script: []lease.Status{heldStatus(lease.Holder{PID: 7, Command: "atomyx run"})}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := session.Attach(ctx, session.AttachDeps{Leases: prober, Clock: &recordingClock{}}, androidDesc("A"))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Attach() error = %v, want context.Canceled", err)
	}
}

func TestAttach_AHolderThatClosesWhileAViewerAttachesEitherAttachesOrReportsTheDeviceFree(t *testing.T) {
	const rounds = 20
	for round := range rounds {
		m := newSocketManager(t, clock.SystemClock{})
		factory := &recordingFactory{events: &eventLog{}}
		s, err := session.Open(context.Background(), socketDeps(t, factory, m), session.Request{Descriptor: androidDesc("A")})
		if err != nil {
			t.Fatal(err)
		}
		closed := make(chan struct{})
		go func() {
			defer close(closed)
			_ = s.Close()
		}()

		type outcome struct {
			result session.AttachResult
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			result, err := session.Attach(context.Background(), session.AttachDeps{Leases: m}, androidDesc("A"))
			done <- outcome{result, err}
		}()
		select {
		case got := <-done:
			if got.err != nil || (got.result.Handle == nil) == !got.result.Free {
				t.Fatalf("round %d: Attach() = %+v, %v, want exactly a handle or Free", round, got.result, got.err)
			}
			if got.result.Handle != nil {
				_ = got.result.Handle.Close()
			}
		case <-time.After(remoteGuard):
			t.Fatalf("round %d: Attach() never returned while the holder closed", round)
		}
		<-closed
	}
}

// gatedDisconnectDriver blocks Disconnect until released, which holds a
// closing session between stopping its socket and releasing its lease.
type gatedDisconnectDriver struct {
	*mockdriver.Driver
	disconnecting chan struct{}
	release       chan struct{}
}

func (d *gatedDisconnectDriver) Disconnect() error {
	close(d.disconnecting)
	<-d.release
	return d.Driver.Disconnect()
}

type gatedDisconnectFactory struct{ drv *gatedDisconnectDriver }

func (f *gatedDisconnectFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *gatedDisconnectFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

func TestAttach_AHolderThatIsExitingWithItsSocketGoneReportsTheDeviceFreeOnceItReleases(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	drv := &gatedDisconnectDriver{Driver: mockdriver.New(), disconnecting: make(chan struct{}), release: make(chan struct{})}
	s, err := session.Open(context.Background(), socketDeps(t, &gatedDisconnectFactory{drv: drv}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = s.Close()
	}()
	awaitClosed(t, "the holder never began to disconnect", drv.disconnecting)
	awaitNoFile(t, socketPathFor(m))

	retried := make(chan struct{})
	var once sync.Once
	prober := &scriptedProber{inner: m, onProbe: func(n int) {
		if n == 2 {
			once.Do(func() { close(retried) })
		}
	}}
	type outcome struct {
		result session.AttachResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := session.Attach(context.Background(), session.AttachDeps{Leases: prober}, androidDesc("A"))
		done <- outcome{result, err}
	}()
	awaitClosed(t, "Attach never probed again after the failed connect", retried)
	close(drv.release)

	select {
	case got := <-done:
		if got.err != nil || !got.result.Free {
			t.Fatalf("Attach() = %+v, %v, want Free once the exiting holder released the lease", got.result, got.err)
		}
	case <-time.After(remoteGuard):
		t.Fatal("Attach never returned after the holder released the lease")
	}
	<-closed
}
