package inspector

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
)

const routeReconnect = "/api/v1/reconnect"

// ErrReleased is returned by a Host's controller methods while the
// device is released after an idle timeout.
var ErrReleased = errors.New("inspector: the device is released")

// ErrLost is returned by a Host's controller methods while the device
// is lost, after the held session reported that it lost the device
// without the inspector choosing to give it up.
var ErrLost = errors.New("inspector: the device is lost")

// Held is a device the inspector holds: a controller, plus the Close
// that gives the device back. A *session.Session is one.
type Held interface {
	session.Controller
	// SetIdle marks the holder idle in the lease metadata, so another
	// process that finds the device held can see it may be borrowed.
	SetIdle() error
	// SetRunning marks the holder as driving the device in the lease
	// metadata. An empty runID means no run is open.
	SetRunning(runID string) error
	// Lost is closed when the held session lost the device without
	// being closed by its owner.
	Lost() <-chan struct{}
	// LostErr reports why the session was lost, and is nil until Lost
	// is closed.
	LostErr() error
	Close() error
}

// Opener acquires the device again after a release. It returns the
// device's own coded error when another process holds it, so the
// inspector reports that code unchanged.
type Opener func(ctx context.Context) (Held, error)

// HostConfig assembles a Host.
type HostConfig struct {
	// Held is the device the inspector already holds.
	Held Held
	// Open re-acquires the device on reconnect.
	Open Opener
	// Clock times the idle release.
	Clock clock.Clock
	// IdleMs is how long the inspector keeps the device without an
	// operator action. Non-positive means session.DefaultMcpIdleTimeoutMs.
	IdleMs int64
	// Logger receives the release and reconnect entries.
	Logger logging.Logger
}

// Host owns the device lifecycle of one inspector: it releases the
// device after an idle timeout and re-acquires it on request. It is
// itself a session.Controller that forwards to the device it holds at
// that moment, so route groups built over a Host never capture a
// controller that a release or a reconnect has replaced.
//
// A held device that reports itself lost is closed and dropped the same
// way an idle release drops it, and the host then answers as lost. A
// command-line run cancels its work when the device is lost and ends,
// but the inspector is a long-lived server, so it stays up, keeps its
// token and waits for the operator to press reconnect, which takes the
// device again through the opener.
//
// Only a route that declares Operator counts as activity. Reading what
// the session holds never keeps the device, mirroring the observe-only
// rule of the MCP server.
type Host struct {
	open   Opener
	clk    clock.Clock
	idleMs int64
	logger logging.Logger

	// opMu serializes a release and a reconnect, so a reconnect never
	// opens the device while the release is still giving it up. It is
	// taken before mu.
	opMu sync.Mutex

	mu        sync.Mutex
	current   Held
	driving   int
	last      session.Info
	lost      bool
	touchedMs int64
	stopWatch context.CancelFunc
	// generation counts the devices the host has adopted, starting at 1.
	// It stamps everything the host reports, so a client can tell a
	// capture of one session from a capture of the next even when the
	// two sessions number their revisions alike.
	generation uint64
}

var _ session.Controller = (*Host)(nil)

// NewHost starts the idle timer over cfg.Held. It fails when a field
// the host cannot work without is missing.
func NewHost(cfg HostConfig) (*Host, error) {
	if cfg.Held == nil || cfg.Open == nil || cfg.Clock == nil || cfg.Logger == nil {
		return nil, errors.New("inspector: a host needs a held device, an opener, a clock and a logger")
	}
	idle := cfg.IdleMs
	if idle <= 0 {
		idle = session.DefaultMcpIdleTimeoutMs
	}
	h := &Host{open: cfg.Open, clk: cfg.Clock, idleMs: idle, logger: cfg.Logger}
	h.mu.Lock()
	h.adoptLocked(cfg.Held)
	h.mu.Unlock()
	h.markIdle(cfg.Held)
	return h, nil
}

// adoptLocked makes held the current device and starts its idle timer
// and its lost watcher.
func (h *Host) adoptLocked(held Held) {
	h.current = held
	h.driving = 0
	h.lost = false
	h.generation++
	h.last = held.Info()
	h.touchedMs = h.clk.NowMs()
	ctx, cancel := context.WithCancel(context.Background())
	h.stopWatch = cancel
	go h.idleLoop(ctx, held)
	go h.lostLoop(ctx, held)
}

// dropLocked forgets the current device after it was released or lost,
// and stops the loops that watch it. The caller closes the device.
func (h *Host) dropLocked(lost bool) {
	h.last = h.current.Info()
	h.current = nil
	h.driving = 0
	h.lost = lost
	h.stopWatch()
}

// lostLoop drops the device and closes it when it reports that it lost
// the device. It ends when the device is replaced or released first.
func (h *Host) lostLoop(ctx context.Context, held Held) {
	select {
	case <-ctx.Done():
		return
	case <-held.Lost():
	}
	h.opMu.Lock()
	defer h.opMu.Unlock()
	h.mu.Lock()
	if h.current != held {
		h.mu.Unlock()
		return
	}
	h.dropLocked(true)
	h.mu.Unlock()
	fields := logging.LogContext{}
	if err := held.LostErr(); err != nil {
		fields["error"] = err.Error()
	}
	h.logger.Warn("inspector.device.lost", fields)
	if err := held.Close(); err != nil {
		h.logger.Warn("inspector.lost.close_failed", logging.LogContext{"error": err.Error()})
	}
}

// idleLoop sleeps until the device would have been idle for the full
// timeout, then releases it. A touch only records the time, so the loop
// wakes, finds the device was used meanwhile, and sleeps the rest. An
// action that is driving the device never lets it be released, and its
// end counts as activity.
func (h *Host) idleLoop(ctx context.Context, held Held) {
	wait := h.idleMs
	for {
		if h.clk.Sleep(ctx, wait) != nil {
			return
		}
		h.opMu.Lock()
		h.mu.Lock()
		if h.current != held {
			h.mu.Unlock()
			h.opMu.Unlock()
			return
		}
		if h.driving > 0 {
			h.mu.Unlock()
			h.opMu.Unlock()
			wait = h.idleMs
			continue
		}
		if remaining := h.touchedMs + h.idleMs - h.clk.NowMs(); remaining > 0 {
			h.mu.Unlock()
			h.opMu.Unlock()
			wait = remaining
			continue
		}
		h.dropLocked(false)
		h.mu.Unlock()
		err := held.Close()
		h.opMu.Unlock()
		if err != nil {
			h.logger.Warn("inspector.idle.release_failed", logging.LogContext{"error": err.Error()})
		}
		h.logger.Info("inspector.idle.released", logging.LogContext{"idleMs": h.idleMs})
		return
	}
}

// markIdle records the holder as idle. A failure only costs the
// metadata, never the operator's action, so it is logged and dropped.
func (h *Host) markIdle(held Held) {
	if err := held.SetIdle(); err != nil {
		h.logger.Warn("inspector.holder.mark_idle_failed", logging.LogContext{"error": err.Error()})
	}
}

// beginDriving marks the holder running while at least one operator
// action that drives the device is in flight, and returns the call that
// ends the action. It refuses with the error for an unavailable device
// when held is no longer the current device, so an action never counts
// against a device that was released, lost or replaced. The holder
// returns to idle when the last in-flight action ends, and the end of
// an action that began on an earlier device changes nothing.
func (h *Host) beginDriving(held Held) (func(), error) {
	h.mu.Lock()
	if h.current != held {
		err := h.unavailableLocked()
		h.mu.Unlock()
		return nil, err
	}
	h.driving++
	first := h.driving == 1
	h.mu.Unlock()
	if first {
		if err := held.SetRunning(""); err != nil {
			h.logger.Warn("inspector.holder.mark_running_failed", logging.LogContext{"error": err.Error()})
		}
	}
	return func() {
		h.mu.Lock()
		last := false
		if h.current == held {
			h.driving--
			h.touchedMs = h.clk.NowMs()
			last = h.driving == 0
		}
		h.mu.Unlock()
		if last {
			h.markIdle(held)
		}
	}, nil
}

// unavailableLocked is the error for a device the host does not hold.
func (h *Host) unavailableLocked() error {
	if h.lost {
		return ErrLost
	}
	return ErrReleased
}

func (h *Host) unavailable() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.unavailableLocked()
}

func (h *Host) touch() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current != nil {
		h.touchedMs = h.clk.NowMs()
	}
}

func (h *Host) held() (Held, bool) {
	held, _, ok := h.heldGeneration()
	return held, ok
}

// heldGeneration returns the current device with the generation it was
// adopted in, read together so a swap between the two reads cannot pair
// a device with the next device's generation.
func (h *Host) heldGeneration() (Held, uint64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current, h.generation, h.current != nil
}

// Info reports the held device, or the last device with state released
// after an idle release and lost after the device was lost.
func (h *Host) Info() session.Info {
	if held, generation, ok := h.heldGeneration(); ok {
		info := held.Info()
		info.Generation = generation
		return info
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	info := h.last
	info.Generation = h.generation
	info.State = session.StateReleased
	if h.lost {
		info.State = session.StateLost
	}
	return info
}

// Gate reports the activity gate of the held device, or nil while the
// host holds none.
func (h *Host) Gate() *session.ActivityGate {
	if held, ok := h.held(); ok {
		return held.Gate()
	}
	return nil
}

// Latest reads the held device's newest capture stamped with the host's
// generation, and is empty while the device is released.
func (h *Host) Latest() session.Snapshot {
	if held, generation, ok := h.heldGeneration(); ok {
		snap := held.Latest()
		snap.Generation = generation
		return snap
	}
	return session.Snapshot{}
}

// Subscribe follows the held device, and fails with ErrReleased or
// ErrLost while the host holds none.
func (h *Host) Subscribe(ctx context.Context) (<-chan session.Observation, error) {
	held, ok := h.held()
	if !ok {
		return nil, h.unavailable()
	}
	return held.Subscribe(ctx)
}

// FreshTree captures from the held device, and fails with ErrReleased
// or ErrLost while the host holds none.
func (h *Host) FreshTree(ctx context.Context) (session.Snapshot, error) {
	held, generation, ok := h.heldGeneration()
	if !ok {
		return session.Snapshot{}, h.unavailable()
	}
	end, err := h.beginDriving(held)
	if err != nil {
		return session.Snapshot{}, err
	}
	defer end()
	snap, err := held.FreshTree(ctx)
	snap.Generation = generation
	return snap, err
}

// Reconnect re-acquires the device after a release or a loss through the opener
// and swaps it in for the routes, without a new token or a new server.
// It does nothing but count as activity when the device is held. The
// opener's error is returned unchanged, so a device another process
// holds reports its own code.
func (h *Host) Reconnect(ctx context.Context) error {
	h.opMu.Lock()
	defer h.opMu.Unlock()
	h.mu.Lock()
	if h.current != nil {
		h.touchedMs = h.clk.NowMs()
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()
	held, err := h.open(ctx)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.adoptLocked(held) //nolint:contextcheck // the idle timer outlives the request that reconnected
	h.mu.Unlock()
	h.markIdle(held)
	h.logger.Info("inspector.reconnected", nil)
	return nil
}

// Close stops the idle timer and gives the device back. It is safe to
// call after a release and more than once.
func (h *Host) Close() error {
	h.opMu.Lock()
	defer h.opMu.Unlock()
	h.mu.Lock()
	held := h.current
	if held != nil {
		h.dropLocked(false)
	}
	h.mu.Unlock()
	if held == nil {
		return nil
	}
	return held.Close()
}

// Routes returns the group that serves groups over the host and adds
// the reconnect route. Every route of groups that declares Operator
// counts as activity, and every route except the session info answers
// a coded 410 while the device is released or lost.
func (h *Host) Routes(groups ...RouteGroup) RouteGroup {
	return func(c session.Controller) []Route {
		var out []Route
		for _, g := range groups {
			for _, r := range g(c) {
				out = append(out, h.guard(r))
			}
		}
		return append(out, Route{Method: http.MethodPost, Path: routeReconnect, Handler: h.reconnectHandler(), Operator: true})
	}
}

func (h *Host) guard(r Route) Route {
	inner := r.Handler
	r.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, ok := h.held(); !ok && r.Path != routeSession {
			writeFailure(w, h.unavailable())
			return
		}
		if r.Operator {
			h.touch()
		}
		inner.ServeHTTP(w, req)
	})
	return r
}

func (h *Host) reconnectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h.Reconnect(r.Context()); err != nil {
			writeFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, h.Info())
	})
}
