package inspector

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/runfolder"
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

// ErrHolderExited is returned by a Host's methods while the host
// follows a holder that has exited, so no device is shown until the
// operator reconnects.
var ErrHolderExited = errors.New("inspector: the process that held the device has exited")

// Target is what a host serves: the view of one device and the Close
// that ends it. A *session.Session that holds the device is one, and so
// is a *session.RemoteHandle that follows the process holding it.
type Target interface {
	Source
	Close() error
}

// Lifecycle is the part of a target that only the process holding the
// device lease has. A host releases the device after an idle timeout,
// records what the holder is doing in the lease metadata, and watches
// for a lost device through it, only for a target that implements it.
// *session.Session implements it.
type Lifecycle interface {
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
}

// Ending is implemented by a target that follows another process. Done
// is closed when that process is gone.
type Ending interface {
	Done() <-chan struct{}
}

// Opener acquires the device again after a release, or attaches to the
// process that now holds it. It returns the device's own coded error
// when the device cannot be reached or held, so the inspector reports
// that code unchanged.
type Opener func(ctx context.Context) (Target, error)

// HostConfig assembles a Host.
type HostConfig struct {
	// Target is the device view the inspector starts with.
	Target Target
	// Open re-acquires the device, or attaches to its holder, on
	// reconnect.
	Open Opener
	// Clock times the idle release.
	Clock clock.Clock
	// IdleMs is how long the inspector keeps the device without an
	// operator action. Non-positive means session.DefaultMcpIdleTimeoutMs.
	IdleMs int64
	// Logger receives the release and reconnect entries.
	Logger logging.Logger
}

// Host owns the device lifecycle of one inspector. It is itself a Source
// and a session.StepSource that forwards to the target it serves at that
// moment, so route groups built over a Host never capture a view that a
// release or a reconnect has replaced.
//
// When the target holds the device lease, the host releases the device
// after an idle timeout and counts a fresh capture as driving the
// device. A target that follows another process has no lease, so the
// host never releases it and only drops it once that process is gone,
// after which the host answers as released.
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

	mu      sync.Mutex
	current Target
	driving int
	last    session.Info
	// gone is why no target is served: ErrReleased, ErrLost or
	// ErrHolderExited. It is nil while a target is served.
	gone      error
	touchedMs int64
	stopWatch context.CancelFunc
	// generation counts the devices the host has adopted, starting at 1.
	// It stamps everything the host reports, so a client can tell a
	// capture of one session from a capture of the next even when the
	// two sessions number their revisions alike.
	generation uint64
}

var (
	_ Source             = (*Host)(nil)
	_ session.StepSource = (*Host)(nil)
)

// NewHost starts serving cfg.Target. It fails when a field the host
// cannot work without is missing.
func NewHost(cfg HostConfig) (*Host, error) {
	if cfg.Target == nil || cfg.Open == nil || cfg.Clock == nil || cfg.Logger == nil {
		return nil, errors.New("inspector: a host needs a target, an opener, a clock and a logger")
	}
	idle := cfg.IdleMs
	if idle <= 0 {
		idle = session.DefaultMcpIdleTimeoutMs
	}
	h := &Host{open: cfg.Open, clk: cfg.Clock, idleMs: idle, logger: cfg.Logger}
	info := cfg.Target.Info()
	h.mu.Lock()
	h.adoptLocked(cfg.Target, info)
	h.mu.Unlock()
	h.markIdle(cfg.Target)
	return h, nil
}

// adoptLocked makes t the current target, with info as its Info read by
// the caller before it took the lock, because a target's Info may be a
// call over a socket. For a target that holds the lease it starts the
// idle timer and the lost watcher, and for one that follows another
// process it starts the watcher of that process.
func (h *Host) adoptLocked(t Target, info session.Info) {
	h.current = t
	h.driving = 0
	h.gone = nil
	h.generation++
	h.last = info
	h.touchedMs = h.clk.NowMs()
	ctx, cancel := context.WithCancel(context.Background())
	h.stopWatch = cancel
	switch v := t.(type) {
	case Lifecycle:
		go h.idleLoop(ctx, t)
		go h.endLoop(ctx, t, v.Lost(), ErrLost)
	case Ending:
		go h.endLoop(ctx, t, v.Done(), ErrHolderExited)
	}
}

// dropLocked forgets the current target for the reason gone, and stops
// the loops that watch it. The caller closes the target, and passes its
// Info read before it took the lock.
func (h *Host) dropLocked(gone error, info session.Info) {
	h.last = info
	h.current = nil
	h.driving = 0
	h.gone = gone
	h.stopWatch()
}

// endLoop drops the target and closes it when ended is closed, with gone
// as the reason. It ends when the target is replaced or released first.
func (h *Host) endLoop(ctx context.Context, t Target, ended <-chan struct{}, gone error) {
	select {
	case <-ctx.Done():
		return
	case <-ended:
	}
	h.opMu.Lock()
	defer h.opMu.Unlock()
	info := t.Info()
	h.mu.Lock()
	if h.current != t {
		h.mu.Unlock()
		return
	}
	h.dropLocked(gone, info)
	h.mu.Unlock()
	fields := logging.LogContext{}
	if lc, ok := t.(Lifecycle); ok {
		if err := lc.LostErr(); err != nil {
			fields["error"] = err.Error()
		}
	}
	h.logger.Warn("inspector.target.ended", fields)
	if err := t.Close(); err != nil {
		h.logger.Warn("inspector.ended.close_failed", logging.LogContext{"error": err.Error()})
	}
}

// idleLoop sleeps until the device would have been idle for the full
// timeout, then releases it. A touch only records the time, so the loop
// wakes, finds the device was used meanwhile, and sleeps the rest. An
// action that is driving the device never lets it be released, and its
// end counts as activity.
func (h *Host) idleLoop(ctx context.Context, held Target) {
	wait := h.idleMs
	for {
		if h.clk.Sleep(ctx, wait) != nil {
			return
		}
		h.opMu.Lock()
		info := held.Info()
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
		h.dropLocked(ErrReleased, info)
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
func (h *Host) markIdle(t Target) {
	lc, ok := t.(Lifecycle)
	if !ok {
		return
	}
	if err := lc.SetIdle(); err != nil {
		h.logger.Warn("inspector.holder.mark_idle_failed", logging.LogContext{"error": err.Error()})
	}
}

// beginDriving marks the holder running while at least one operator
// action that drives the device is in flight, and returns the call that
// ends the action. It refuses with the error for an unavailable device
// when held is no longer the current device, so an action never counts
// against a device that was released, lost or replaced. The holder
// returns to idle when the last in-flight action ends, and the end of
// an action that began on an earlier device changes nothing. A target
// without a lifecycle has no holder state to record, so the action only
// counts as activity.
func (h *Host) beginDriving(held Target) (func(), error) {
	h.mu.Lock()
	if h.current != held {
		err := h.unavailableLocked()
		h.mu.Unlock()
		return nil, err
	}
	h.driving++
	first := h.driving == 1
	h.mu.Unlock()
	lc, tracked := held.(Lifecycle)
	if first && tracked {
		if err := lc.SetRunning(""); err != nil {
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
		if last && tracked {
			h.markIdle(held)
		}
	}, nil
}

// unavailableLocked is the error for a device the host does not hold.
func (h *Host) unavailableLocked() error {
	if h.gone == nil {
		return ErrReleased
	}
	return h.gone
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

func (h *Host) held() (Target, bool) {
	held, _, ok := h.heldGeneration()
	return held, ok
}

// heldGeneration returns the current device with the generation it was
// adopted in, read together so a swap between the two reads cannot pair
// a device with the next device's generation.
func (h *Host) heldGeneration() (Target, uint64, bool) {
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
	if errors.Is(h.gone, ErrLost) {
		info.State = session.StateLost
	}
	return info
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

// Subscribe follows the served device, and fails with the reason the
// host serves none while it serves none.
func (h *Host) Subscribe(ctx context.Context) (<-chan session.Observation, error) {
	held, ok := h.held()
	if !ok {
		return nil, h.unavailable()
	}
	return held.Subscribe(ctx)
}

// StepsSince returns the step lines the served target holds after line
// sinceSeq of run runID. It returns no lines while the target does not
// follow step lines, and fails with the reason the host serves none
// while it serves none.
func (h *Host) StepsSince(runID string, sinceSeq int64) ([]runfolder.StepLine, bool, error) {
	held, ok := h.held()
	if !ok {
		return nil, false, h.unavailable()
	}
	if steps, ok := held.(session.StepSource); ok {
		return steps.StepsSince(runID, sinceSeq)
	}
	return nil, false, nil
}

// SubscribeSince follows the served target and its step lines. A target
// that does not follow step lines is followed with Subscribe.
func (h *Host) SubscribeSince(ctx context.Context, runID string, sinceSeq int64) (<-chan session.Observation, error) {
	held, ok := h.held()
	if !ok {
		return nil, h.unavailable()
	}
	if steps, ok := held.(session.StepSource); ok {
		return steps.SubscribeSince(ctx, runID, sinceSeq)
	}
	return held.Subscribe(ctx)
}

// FreshTree captures from the served device, and fails with the reason
// the host serves none while it serves none.
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
	info := held.Info()
	h.mu.Lock()
	h.adoptLocked(held, info) //nolint:contextcheck // the idle timer outlives the request that reconnected
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
	held, ok := h.held()
	if !ok {
		return nil
	}
	info := held.Info()
	h.mu.Lock()
	h.dropLocked(ErrReleased, info)
	h.mu.Unlock()
	return held.Close()
}

// Routes returns the group that serves groups over the host and adds
// the reconnect route. Every route of groups that declares Operator
// counts as activity, and every route except the session info answers
// a coded 410 while the device is released or lost.
func (h *Host) Routes(groups ...RouteGroup) RouteGroup {
	return func(c Source) []Route {
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
