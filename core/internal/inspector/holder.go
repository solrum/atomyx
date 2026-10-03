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
	touchedMs int64
	stopIdle  context.CancelFunc
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

// adoptLocked makes held the current device and starts its idle timer.
func (h *Host) adoptLocked(held Held) {
	h.current = held
	h.driving = 0
	h.last = held.Info()
	h.touchedMs = h.clk.NowMs()
	ctx, cancel := context.WithCancel(context.Background())
	h.stopIdle = cancel
	go h.idleLoop(ctx, held)
}

// idleLoop sleeps until the device would have been idle for the full
// timeout, then releases it. A touch only records the time, so the loop
// wakes, finds the device was used meanwhile, and sleeps the rest.
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
		if remaining := h.touchedMs + h.idleMs - h.clk.NowMs(); remaining > 0 {
			h.mu.Unlock()
			h.opMu.Unlock()
			wait = remaining
			continue
		}
		h.current = nil
		h.last = held.Info()
		h.stopIdle()
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
// ends the action. The holder returns to idle when the last in-flight
// action ends, unless a release or a reconnect replaced the device.
func (h *Host) beginDriving(held Held) func() {
	h.mu.Lock()
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
			last = h.driving == 0
		}
		h.mu.Unlock()
		if last {
			h.markIdle(held)
		}
	}
}

func (h *Host) touch() {

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current != nil {
		h.touchedMs = h.clk.NowMs()
	}
}

func (h *Host) held() (Held, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current, h.current != nil
}

// Info reports the held device, or the last device with state released.
func (h *Host) Info() session.Info {
	if held, ok := h.held(); ok {
		return held.Info()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	info := h.last
	info.State = session.StateReleased
	return info
}

// Latest reads the held device's newest capture, and is empty while the
// device is released.
func (h *Host) Latest() session.Snapshot {
	if held, ok := h.held(); ok {
		return held.Latest()
	}
	return session.Snapshot{}
}

// Subscribe follows the held device, and fails with ErrReleased while
// it is released.
func (h *Host) Subscribe(ctx context.Context) (<-chan session.Observation, error) {
	held, ok := h.held()
	if !ok {
		return nil, ErrReleased
	}
	return held.Subscribe(ctx)
}

// FreshTree captures from the held device, and fails with ErrReleased
// while it is released.
func (h *Host) FreshTree(ctx context.Context) (session.Snapshot, error) {
	held, ok := h.held()
	if !ok {
		return session.Snapshot{}, ErrReleased
	}
	defer h.beginDriving(held)()
	return held.FreshTree(ctx)
}

// Reconnect re-acquires the device after a release through the opener
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
	h.current = nil
	if held != nil {
		h.last = held.Info()
		h.stopIdle()
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
// a coded 410 while the device is released.
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
			writeFailure(w, ErrReleased)
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
