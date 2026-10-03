package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/shutdown"
)

// ErrDisconnectTimeout reports that the driver's Disconnect did not
// finish inside the shared shutdown grace period.
var ErrDisconnectTimeout = errors.New("device disconnect did not finish in time")

// ErrAttachStopTimeout reports that the attach server did not stop inside
// the shared shutdown grace period, for example because a viewer's request
// is stuck in a driver call that never returns.
var ErrAttachStopTimeout = errors.New("attach server did not stop in time")

// ErrDeviceLost is wrapped by every cause LostErr reports, so a caller
// that holds only a cancellation cause can tell a lost device from a
// user stop. The wrapped cause names what ended the session: the lease
// (lease.ErrLost) or a process the control plane depends on
// (*device.ProcessExitError).
var ErrDeviceLost = errors.New("device lost")

// Session is a device a process owns: the lease, the connected driver
// with its Orchestra, and the watcher guarding the lease. Obtain one
// through Open and end it with Close.
type Session struct {
	deps     Deps
	desc     device.Descriptor
	identity device.Identity
	lease    *lease.Lease
	built    device.Built
	hub      *Hub
	gate     *ActivityGate

	watchCancel context.CancelFunc
	watchers    sync.WaitGroup

	lostOnce sync.Once
	lost     chan struct{}
	mu       sync.Mutex
	lostErr  error
	saved    *LaunchState
	// activity is StateIdle or StateRunning. A session starts running, so
	// only an owner that manages the activity gate ever marks it idle.
	// Open records the starting value in the lease holder record.
	activity HolderState

	relaunched bool

	// socket is the attach socket path recorded in the lease metadata,
	// empty when the session serves none. It is set before the session is
	// shared and never changes.
	socket     string
	attach     *attachListener
	attachDone bool
	attachOnce sync.Once

	// interventionMu guards the pending interventions and the recorder, and
	// is held while a line is appended so entries reach the run folder in
	// the order they were recorded.
	interventionMu sync.Mutex
	interventions  []Intervention
	recorder       InterventionRecorder

	closeOnce sync.Once
	closeErr  error
	closed    chan struct{}
}

func newSession(ctx context.Context, deps Deps, desc device.Descriptor, identity device.Identity, held *lease.Lease, built device.Built, hub *Hub, gate *ActivityGate) *Session {
	s := &Session{
		deps: deps, desc: desc, identity: identity, lease: held, built: built, hub: hub, gate: gate,
		activity: StateRunning,
		lost:     make(chan struct{}),
		closed:   make(chan struct{}),
	}
	// The watcher outlives the Open call that started it, so it keeps
	// ctx's values but not its cancellation.
	watchCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.watchCancel = cancel
	if held != nil {
		s.watchers.Go(func() { held.Watch(watchCtx, s.markLost) })
	}
	if built.Lost != nil {
		s.watchers.Go(func() {
			select {
			case cause := <-built.Lost:
				s.markLost(cause)
			case <-watchCtx.Done():
			}
		})
	}
	return s
}

// Descriptor reports the device the session drives.
func (s *Session) Descriptor() device.Descriptor { return s.desc }

// Identity reports the canonical hardware identity the lease keys on.
func (s *Session) Identity() device.Identity { return s.identity }

// Built reports the connected driver and its Orchestra.
func (s *Session) Built() device.Built { return s.built }

// Orchestra reports the session's Orchestra.
func (s *Session) Orchestra() *orchestra.Orchestra { return s.built.Orchestra }

// Lost is closed when the session lost the device without choosing to:
// the lease file was replaced under it and re-acquiring failed, or a
// process the control plane depends on exited on its own. The owner
// decides what to do. Normally it ends the run in flight and closes the
// session.
func (s *Session) Lost() <-chan struct{} { return s.lost }

// Closed is closed once Close has finished, so a goroutine that waits on
// Lost can also end when the owner closes the session itself.
func (s *Session) Closed() <-chan struct{} { return s.closed }

// LostErr reports why the session was lost, or nil while it is held.
// A non-nil result wraps ErrDeviceLost.
func (s *Session) LostErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lostErr
}

func (s *Session) markLost(err error) {
	s.lostOnce.Do(func() {
		wrapped := fmt.Errorf("%w: %w", ErrDeviceLost, err)
		s.mu.Lock()
		s.lostErr = wrapped
		s.mu.Unlock()
		s.deps.Logger.Warn("lost the device", logging.LogContext{"id": s.desc.ID, "error": err.Error()})
		close(s.lost)
		go s.stopAttach()
	})
}

// Relaunched reports that opening this session launched the app of an
// earlier session afresh on a new runner, so the app's in-memory state is
// gone.
func (s *Session) Relaunched() bool { return s.relaunched }

// LaunchState reports the launched-app state of the session's driver,
// or the state captured when the session was closed. The zero value
// means the driver tracks none.
func (s *Session) LaunchState() LaunchState {
	s.mu.Lock()
	saved := s.saved
	s.mu.Unlock()
	if saved != nil {
		return *saved
	}
	return s.liveLaunchState()
}

func (s *Session) liveLaunchState() LaunchState {
	carrier, ok := driver.As[launchedAppCarrier](s.built.Driver)
	if !ok {
		return LaunchState{}
	}
	bundleID, runnerID := carrier.LaunchedApp()
	return LaunchState{BundleID: bundleID, RunnerID: runnerID}
}

// SetRunning marks the holder as working, optionally with an open run.
// It sets the state Info reports and records it in the lease metadata,
// so another process that finds the device held can say why. An empty
// runID means no run is open.
func (s *Session) SetRunning(runID string) error {
	s.setActivity(StateRunning)
	return s.updateHolder(func(h *lease.Holder) {
		h.IdleSince = nil
		h.RunID = runID
	})
}

// SetIdle marks the holder as idle at the current time, in the state
// Info reports and in the lease metadata.
func (s *Session) SetIdle() error {
	s.setActivity(StateIdle)
	now := time.UnixMilli(s.deps.Clock.NowMs()).UTC()
	return s.updateHolder(func(h *lease.Holder) {
		h.IdleSince = &now
		h.RunID = ""
	})
}

// persistActivity writes the session's current activity to the lease
// holder record, so the record tells idle from running from the moment
// the session exists.
func (s *Session) persistActivity() error {
	s.mu.Lock()
	state := s.activity
	s.mu.Unlock()
	if state == StateIdle {
		return s.SetIdle()
	}
	return s.SetRunning("")
}

func (s *Session) setActivity(state HolderState) {
	s.mu.Lock()
	s.activity = state
	s.mu.Unlock()
}

func (s *Session) updateHolder(mutate func(*lease.Holder)) error {
	if s.lease == nil {
		return nil
	}
	err := s.lease.UpdateHolder(func(h *lease.Holder) {
		mutate(h)
		h.Socket = s.socket
	})
	if errors.Is(err, lease.ErrReleased) {
		return nil
	}
	return err
}

// Close ends the session. It stops the lease watcher, then stops the
// attach server and disconnects the driver at the same time, then
// releases the lease.
//
// Stopping the server closes every viewer connection and removes the
// socket file. Disconnecting the driver stops a runner the factory
// launched. The two run side by side because a viewer's fresh capture can
// be stuck in a driver call that only Disconnect ends, so the disconnect
// must never wait behind the server. Both are bounded by the shared
// shutdown grace period.
//
// Close is idempotent and returns the first call's result.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.watchCancel()
		s.watchers.Wait()

		state := s.liveLaunchState()
		s.mu.Lock()
		s.saved = &state
		s.mu.Unlock()

		disconnected := make(chan error, 1)
		attachStopped := make(chan struct{})
		finished := shutdown.Bounded(s.deps.Clock, shutdown.GracePeriod, func() {
			go func() {
				defer close(attachStopped)
				s.stopAttach()
			}()
			disconnected <- s.built.Driver.Disconnect()
			<-attachStopped
		})
		var disconnectErr error
		select {
		case disconnectErr = <-disconnected:
		default:
			disconnectErr = ErrDisconnectTimeout
		}
		if !finished && !errors.Is(disconnectErr, ErrDisconnectTimeout) {
			disconnectErr = errors.Join(disconnectErr, ErrAttachStopTimeout)
		}
		s.hub.Close()
		var releaseErr error
		if s.lease != nil {
			releaseErr = s.lease.Release()
		}
		s.closeErr = errors.Join(disconnectErr, releaseErr)
		close(s.closed)
	})
	return s.closeErr
}
