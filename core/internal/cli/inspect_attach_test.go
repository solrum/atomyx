package cli

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/wire"
)

// skipClock is a clock whose sleeps return at once and add to the time
// it reports, so a wait whose budget is minutes of real time ends in a
// few probes.
type skipClock struct{ now atomic.Int64 }

func (c *skipClock) NowMs() int64 { return c.now.Load() }

func (c *skipClock) Sleep(ctx context.Context, ms int64) error {
	c.now.Add(ms)
	return ctx.Err()
}

// idleBudgetClock behaves like skipClock except that a sleep of the
// device acquisition budget waits for its context, because that sleep is
// the timer of the acquisition and must not fire while the lease retries
// at the pace of the instant clock.
type idleBudgetClock struct{ *skipClock }

func (c idleBudgetClock) Sleep(ctx context.Context, ms int64) error {
	if ms == session.DefaultDeviceAcquireTimeoutMs {
		<-ctx.Done()
		return ctx.Err()
	}
	return c.skipClock.Sleep(ctx, ms)
}

func servingSockets(manager *lease.Manager) func(*Deps) {
	return func(d *Deps) { d.Session.Sockets = manager }
}

func withAttachClock(manager *lease.Manager, clk *skipClock) func(*Deps) {
	return func(d *Deps) {
		d.Session.Sockets = manager
		d.Session.Clock = clk
	}
}

// holdDeviceServingASocket takes the device the way another Atomyx
// process would, with an attach socket bound. The new holder is running.
func holdDeviceServingASocket(t *testing.T, manager *lease.Manager) *deviceSession {
	t.Helper()
	var out, errOut bytes.Buffer
	deps := leaseDeps(t, manager, &out, &errOut)
	deps.Session.Sockets = manager
	deps.Factory = singleMockDriverFactory{driver: mockdriver.New().StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})}
	opened, err := openDevice(context.Background(), deps, "atomyx run", device.Descriptor{Platform: device.PlatformAndroid, ID: "emulator-5554"})
	if err != nil {
		t.Fatalf("the first process could not take the device: %v", err)
	}
	t.Cleanup(opened.Close)
	return opened
}

// awaitInspectURL returns the bootstrap URL the command prints, and fails
// when the command exits first.
func awaitInspectURL(t *testing.T, run *inspectRun) string {
	t.Helper()
	for {
		select {
		case line := <-run.stdout.lines:
			if strings.Contains(line, "http://") {
				return line
			}
		case code := <-run.exit:
			t.Fatalf("inspect exited with %d before it printed a URL", code)
		}
	}
}

type sessionBody struct {
	State  string
	Holder struct{ Command string }
}

func (c *inspectClient) session(t *testing.T) sessionBody {
	t.Helper()
	var info sessionBody
	if status := c.send(t, http.MethodGet, "/api/v1/session", "", &info); status != http.StatusOK {
		t.Fatalf("session = %d", status)
	}
	return info
}

// codeOfFailure reads the code of an error body the page API answers
// with.
func (c *inspectClient) failureCode(t *testing.T, method, path string) (int, string) {
	t.Helper()
	var body struct{ Error struct{ Code string } }
	status := c.send(t, method, path, "", &body)
	return status, body.Error.Code
}

func TestInspect_AHeldDeviceIsWatchedAndAFreshCaptureWaitsForAnIdleHolder(t *testing.T) {
	manager := newShortLeaseManager(t)
	holder := holdDeviceServingASocket(t, manager)
	run := startInspectWith(t, manager, heldDevice, nil, servingSockets(manager))
	client := connectClient(t, awaitInspectURL(t, run))

	info := client.session(t)
	if info.Holder.Command != "atomyx run" || info.State != string(session.StateRunning) {
		t.Fatalf("session = %+v, want the holder's command and the state running", info)
	}
	if status, code := client.failureCode(t, http.MethodPost, "/api/v1/tree/fresh"); status != http.StatusConflict || code != diag.CodeHolderBusy {
		t.Fatalf("fresh while the holder runs = %d %s, want 409 %s", status, code, diag.CodeHolderBusy)
	}
	if err := holder.SetIdle(); err != nil {
		t.Fatal(err)
	}
	if status := client.send(t, http.MethodPost, "/api/v1/tree/fresh", "", nil); status != http.StatusOK {
		t.Fatalf("fresh while the holder is idle = %d, want 200", status)
	}
	if code := run.stop(syscall.SIGINT); code == 0 {
		t.Fatalf("exit = %d, want the signal code", code)
	}
	if !run.stderr.wrote("Ctrl-C stops watching the device") || run.stderr.wrote("Ctrl-C releases the device") {
		t.Fatal("a viewer was not told that Ctrl-C stops watching, or was told it releases the device")
	}
	if err := run.otherProcessOpens(t); err == nil || !strings.Contains(err.Error(), "atomyx run") {
		t.Fatalf("after the inspector stopped, another open = %v, want the original holder to still hold the device", err)
	}
}

func TestInspect_TheTypedAttachFailuresPrintAnOperatorMessageAndExitOne(t *testing.T) {
	key := lease.KeyFor(string(device.PlatformAndroid), "emulator-5554")
	cases := []struct {
		name string
		hold func(t *testing.T, manager *lease.Manager)
		want string
	}{
		{
			name: "a holder that serves no socket",
			hold: func(t *testing.T, manager *lease.Manager) { holdDevice(t, manager) },
			want: "serves no attach socket",
		},
		{
			name: "a holder that has not published a socket",
			hold: func(t *testing.T, manager *lease.Manager) {
				holdLeaseDirectly(t, manager, key, lease.Holder{PID: os.Getpid(), Command: "atomyx run", Role: "cli", StartedAt: time.Now()})
			},
			want: "still starting",
		},
		{
			name: "a holder whose socket does not answer",
			hold: func(t *testing.T, manager *lease.Manager) {
				holdLeaseDirectly(t, manager, key, lease.Holder{PID: os.Getpid(), Command: "atomyx run", Role: "cli", StartedAt: time.Now(), Socket: manager.SocketPath(key)})
			},
			want: "did not accept a connection",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := &skipClock{}
			manager := newShortLeaseManagerWithClock(t, clk)
			tc.hold(t, manager)
			run := startInspectWith(t, manager, heldDevice, nil, withAttachClock(manager, clk))

			if code := <-run.exit; code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}

			message := drainLines(run.stderr)
			if !strings.Contains(message, tc.want) || !strings.Contains(message, diag.CodeDeviceHeld) {
				t.Errorf("stderr %q lacks %q or %s", message, tc.want, diag.CodeDeviceHeld)
			}
			select {
			case out := <-run.stdout.lines:
				t.Fatalf("stdout printed %q for a device that was never reached", out)
			default:
			}
		})
	}
}

// drainLines returns the lines the sink holds now, without waiting for
// more. A command that has exited has written every line it will write.
func drainLines(s *lineSink) string {
	var out []string
	for {
		select {
		case line := <-s.lines:
			out = append(out, line)
		default:
			return strings.Join(out, "\n")
		}
	}
}

func holdLeaseDirectly(t *testing.T, manager *lease.Manager, key lease.Key, holder lease.Holder) {
	t.Helper()
	l, err := manager.Acquire(context.Background(), key, holder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Release() })
}

func TestInspect_AnExitedHolderShowsReleasedAndReconnectAcquiresTheFreeDevice(t *testing.T) {
	manager := newShortLeaseManager(t)
	holder := holdDeviceServingASocket(t, manager)
	run := startInspectWith(t, manager, heldDevice, nil, servingSockets(manager))
	client := connectClient(t, awaitInspectURL(t, run))

	holder.Close()
	run.stderr.awaitLine(t, "inspector.target.ended")

	if info := client.session(t); info.State != string(session.StateReleased) {
		t.Fatalf("state after the holder exited = %q, want released", info.State)
	}
	if status, code := client.failureCode(t, http.MethodGet, "/api/v1/tree"); status != http.StatusGone || code != "HOLDER_EXITED" {
		t.Fatalf("tree after the holder exited = %d %s, want 410 HOLDER_EXITED", status, code)
	}
	if err := run.otherProcessOpens(t); err != nil {
		t.Fatalf("the device was not free after the holder exited, so the inspector took it by itself: %v", err)
	}
	if status := client.send(t, http.MethodPost, "/api/v1/reconnect", "", nil); status != http.StatusOK {
		t.Fatalf("reconnect = %d, want 200", status)
	}
	if info := client.session(t); info.State != string(session.StateIdle) || info.Holder.Command != inspectCommandName {
		t.Fatalf("session after reconnect = %+v, want the inspector to hold the device idle", info)
	}
	if err := run.otherProcessOpens(t); err == nil {
		t.Fatal("a second process opened a device the inspector reacquired")
	}
	run.stop(syscall.SIGINT)
}

func TestInspect_ReconnectWatchesTheNextHolderOfTheDevice(t *testing.T) {
	manager := newShortLeaseManager(t)
	first := holdDeviceServingASocket(t, manager)
	run := startInspectWith(t, manager, heldDevice, nil, servingSockets(manager))
	client := connectClient(t, awaitInspectURL(t, run))
	first.Close()
	run.stderr.awaitLine(t, "inspector.target.ended")
	next := holdDeviceServingASocket(t, manager)

	if status := client.send(t, http.MethodPost, "/api/v1/reconnect", "", nil); status != http.StatusOK {
		t.Fatalf("reconnect = %d, want 200", status)
	}

	info := client.session(t)
	if info.Holder.Command != "atomyx run" || info.State != string(session.StateRunning) {
		t.Fatalf("session after reconnect = %+v, want the next holder's command and state", info)
	}
	next.Close()
	run.stderr.awaitLine(t, "inspector.target.ended")
	run.stop(syscall.SIGINT)
}

func TestInspect_ReconnectOnADeviceWhoseHolderServesNoSocketKeepsTheReleasedPage(t *testing.T) {
	clk := &skipClock{}
	manager := newShortLeaseManagerWithClock(t, clk)
	first := holdDeviceServingASocket(t, manager)
	run := startInspectWith(t, manager, heldDevice, nil, func(d *Deps) {
		d.Session.Sockets = manager
		d.Session.Clock = idleBudgetClock{clk}
	})
	client := connectClient(t, awaitInspectURL(t, run))
	first.Close()
	run.stderr.awaitLine(t, "inspector.target.ended")
	holdDevice(t, manager)

	status, code := client.failureCode(t, http.MethodPost, "/api/v1/reconnect")

	if status != http.StatusConflict || code != diag.CodeDeviceHeld {
		t.Fatalf("reconnect = %d %s, want 409 %s", status, code, diag.CodeDeviceHeld)
	}
	if info := client.session(t); info.State != string(session.StateReleased) {
		t.Fatalf("state = %q, want the page to stay released", info.State)
	}
	run.stop(syscall.SIGINT)
}

// stateProbeDriver reads the holder state recorded in the lease each
// time the device is asked for its hierarchy while armed.
type stateProbeDriver struct {
	*mockdriver.Driver
	armed atomic.Bool
	probe func() string
	mu    sync.Mutex
	seen  []string
}

func (d *stateProbeDriver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	if d.armed.Load() {
		state := d.probe()
		d.mu.Lock()
		d.seen = append(d.seen, state)
		d.mu.Unlock()
	}
	return d.Driver.Hierarchy(ctx)
}

type stateProbeFactory struct{ drv *stateProbeDriver }

func (f stateProbeFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f stateProbeFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

func TestInspect_AViewersFreshCaptureIsCountedAsTheInspectorDrivingTheDevice(t *testing.T) {
	manager := newShortLeaseManager(t)
	key := lease.KeyFor(string(device.PlatformAndroid), "emulator-5554")
	drv := &stateProbeDriver{Driver: mockdriver.New().StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})}
	drv.probe = func() string {
		status, err := manager.Probe(context.Background(), key)
		if err != nil {
			return err.Error()
		}
		return status.Holder.State()
	}
	run := startInspectWith(t, manager, heldDevice, nil, func(d *Deps) {
		d.Session.Sockets = manager
		d.Factory = stateProbeFactory{drv: drv}
	})
	awaitInspectURL(t, run)
	viewer := sessiontest.DialViewer(t, holderSocket(t, manager), "atomyx inspect")
	drv.armed.Store(true)

	if err := viewer.TreeFresh(); err != nil {
		t.Fatalf("a viewer's fresh capture on the inspector's socket: %v", err)
	}

	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.seen) == 0 || drv.seen[0] != "running" {
		t.Fatalf("holder state while the viewer's capture read the device = %v, want running", drv.seen)
	}
	run.stop(syscall.SIGINT)
}

// stoppedClock reports a time only the test moves, and its sleeps last
// until their context ends, so no timer elapses by itself.
type stoppedClock struct{ now atomic.Int64 }

func (c *stoppedClock) NowMs() int64 { return c.now.Load() }

func (c *stoppedClock) Sleep(ctx context.Context, _ int64) error {
	<-ctx.Done()
	return ctx.Err()
}

// timeSpendingProber reports a free device after it has used up spendMs
// of the clock, the way a wait for a holder would, while every acquire
// goes to the real lease manager.
type timeSpendingProber struct {
	*lease.Manager
	clk     *stoppedClock
	spendMs int64
	probes  atomic.Int32
}

func (p *timeSpendingProber) Probe(context.Context, lease.Key) (lease.Status, error) {
	p.probes.Add(1)
	p.clk.now.Add(p.spendMs)
	return lease.Status{}, nil
}

func TestInspectOpener_TheAttemptsShareOneWaitBudget(t *testing.T) {
	clk := &stoppedClock{}
	manager := newShortLeaseManager(t)
	holdDevice(t, manager)
	prober := &timeSpendingProber{Manager: manager, clk: clk, spendMs: inspectAttachBudgetMs * 6 / 10}
	deps := leaseDeps(t, manager, nil, nil)
	deps.Session.Leases = prober
	deps.Session.Clock = clk
	deps.Factory = singleMockDriverFactory{driver: mockdriver.New()}

	_, err := inspectOpener(deps, device.Descriptor{Platform: device.PlatformAndroid, ID: "emulator-5554"}, nil, nil)(context.Background())

	if code, _, _ := diag.Describe(err); code != diag.CodeDeviceHeld {
		t.Fatalf("open = %v, want %s", err, diag.CodeDeviceHeld)
	}
	if got := prober.probes.Load(); got != 2 {
		t.Fatalf("probes = %d, want 2 because two attempts spend the whole budget", got)
	}
}
