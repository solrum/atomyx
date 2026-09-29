package device

import (
	"context"
	"sync"

	"dev.atomyx/internal/iossimdriver"
	"dev.atomyx/internal/process"
)

// SimHidState is one UDID's current atomyx-sim-hid helper lifecycle
// state, matching the sidecar's iosSimHidStatus wire values byte for
// byte: "idle" before a helper has ever been asked to start,
// "spawning" while a start is in flight, "ready" once its handshake
// resolved a port, and "failed" when the most recent attempt did not.
type SimHidState string

const (
	SimHidStateIdle     SimHidState = "idle"
	SimHidStateSpawning SimHidState = "spawning"
	SimHidStateReady    SimHidState = "ready"
	SimHidStateFailed   SimHidState = "failed"
)

// SimHidStatus is one UDID's current atomyx-sim-hid helper state, as
// SimHidManager.Status reports it. Port only carries the helper's
// listening port once HasPort is true; a UDID that has never reached
// the ready state has no port to report, and Port stays its zero
// value.
type SimHidStatus struct {
	UDID    string
	State   SimHidState
	Port    int
	HasPort bool
	Message string
}

// simHidLauncher is the seam SimHidManager depends on to actually
// spawn the helper and wait for its handshake.
// *iossimdriver.SimHidLauncher implements it in production, and a
// test substitutes a fake so the manager's state machine is exercised
// without a real helper binary or process.
type simHidLauncher interface {
	Launch(ctx context.Context, udid string) (iossimdriver.SimHidLaunchResult, error)
}

// simHidStarter is the seam DriverFactory depends on to start the
// atomyx-sim-hid helper for a qualifying iOS simulator target.
// *SimHidManager implements it in production, and a factory test
// substitutes a fake to control success or failure without spawning
// any process.
type simHidStarter interface {
	StartSimHid(ctx context.Context, udid string) (int, error)
}

// simHidEntry is one UDID's in-flight or resolved start attempt.
// cancel stops a still-spawning attempt and is used by Dispose, and
// kill stops an already-running helper and is set only once state
// reaches ready.
type simHidEntry struct {
	state   SimHidState
	port    int
	message string
	cancel  context.CancelFunc
	kill    func() error

	done chan struct{}
	err  error
}

// SimHidManager owns the in-memory per-UDID state of the
// atomyx-sim-hid helper: at most one spawn attempt in flight per
// UDID, shared by every concurrent StartSimHid caller for that UDID.
// device.DriverFactory calls StartSimHid while building an iOS
// simulator driver that has opted into the direct-HID path. The
// sidecar package reads Status through the SimHidStatusProvider
// interface it declares for exactly this purpose, so device never
// imports sidecar.
//
// SimHidManager is safe for concurrent use.
type SimHidManager struct {
	launcher simHidLauncher

	mu      sync.Mutex
	entries map[string]*simHidEntry
}

// NewSimHidManager constructs a SimHidManager backed by the real
// atomyx-sim-hid launcher, spawning helper processes through bg.
func NewSimHidManager(bg process.BackgroundRunner) *SimHidManager {
	return newSimHidManagerWithLauncher(iossimdriver.NewSimHidLauncher(bg))
}

func newSimHidManagerWithLauncher(launcher simHidLauncher) *SimHidManager {
	return &SimHidManager{launcher: launcher, entries: make(map[string]*simHidEntry)}
}

var _ simHidStarter = (*SimHidManager)(nil)

// Status reports udid's current helper state, defaulting to idle when
// the helper has never been asked to start.
func (m *SimHidManager) Status(udid string) SimHidStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked(udid)
}

func (m *SimHidManager) statusLocked(udid string) SimHidStatus {
	entry, ok := m.entries[udid]
	if !ok {
		return SimHidStatus{UDID: udid, State: SimHidStateIdle}
	}
	return SimHidStatus{UDID: udid, State: entry.state, Port: entry.port, HasPort: entry.state == SimHidStateReady, Message: entry.message}
}

// StartSimHid starts the atomyx-sim-hid helper for udid unless an
// attempt is already ready or in flight, in which case every caller
// shares that single attempt's outcome. It blocks until the helper's
// handshake resolves a port, the attempt fails, or ctx is done.
// Canceling ctx only stops this caller from waiting: the underlying
// spawn attempt keeps running for whoever else is waiting on it, or
// until Dispose.
//
// A UDID whose most recent attempt failed starts a fresh attempt on
// the next call, matching a plain retry.
func (m *SimHidManager) StartSimHid(ctx context.Context, udid string) (int, error) {
	m.mu.Lock()
	entry, ok := m.entries[udid]
	if ok && (entry.state == SimHidStateReady || entry.state == SimHidStateSpawning) {
		m.mu.Unlock()
		return waitSimHid(ctx, entry)
	}

	spawnCtx, cancel := context.WithCancel(context.Background())
	entry = &simHidEntry{state: SimHidStateSpawning, cancel: cancel, done: make(chan struct{})}
	m.entries[udid] = entry
	m.mu.Unlock()

	go m.runLaunch(spawnCtx, udid, entry) //nolint:contextcheck // spawnCtx deliberately outlives ctx: see the doc comment above on why the attempt keeps running for other waiters after this caller's ctx is done

	return waitSimHid(ctx, entry)
}

func (m *SimHidManager) runLaunch(ctx context.Context, udid string, entry *simHidEntry) {
	result, err := m.launcher.Launch(ctx, udid)

	m.mu.Lock()
	if err != nil {
		entry.state = SimHidStateFailed
		entry.message = err.Error()
	} else {
		entry.state = SimHidStateReady
		entry.port = result.Port
		entry.kill = result.Kill
	}
	entry.err = err
	m.mu.Unlock()

	close(entry.done)
}

func waitSimHid(ctx context.Context, entry *simHidEntry) (int, error) {
	select {
	case <-entry.done:
		return entry.port, entry.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// Dispose stops every helper attempt this manager started: a still
// spawning attempt is canceled, and an already-running helper is
// killed. It leaves the manager with no tracked UDIDs, so a later
// StartSimHid call starts a fresh attempt.
func (m *SimHidManager) Dispose() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.entries {
		if entry.cancel != nil {
			entry.cancel()
		}
		if entry.kill != nil {
			_ = entry.kill()
		}
	}
	m.entries = make(map[string]*simHidEntry)
}
