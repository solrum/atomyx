package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/shutdown"
)

// ErrDisconnectTimeout reports that the driver's Disconnect did not
// finish inside the shared shutdown grace period.
var ErrDisconnectTimeout = errors.New("device disconnect did not finish in time")

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

	watchCancel context.CancelFunc
	watchers    sync.WaitGroup

	lostOnce sync.Once
	lost     chan struct{}
	mu       sync.Mutex
	lostErr  error
	saved    *LaunchState

	relaunched bool

	closeOnce sync.Once
	closeErr  error
	closed    chan struct{}
}

func newSession(ctx context.Context, deps Deps, desc device.Descriptor, identity device.Identity, held *lease.Lease, built device.Built) *Session {
	s := &Session{
		deps: deps, desc: desc, identity: identity, lease: held, built: built,
		lost:   make(chan struct{}),
		closed: make(chan struct{}),
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
// process the control plane depends on exited on its own. The owner decides what to do, normally by ending the run in flight and
// closing the session.
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
	carrier, ok := s.built.Driver.(launchedAppCarrier)
	if !ok {
		return LaunchState{}
	}
	bundleID, runnerID := carrier.LaunchedApp()
	return LaunchState{BundleID: bundleID, RunnerID: runnerID}
}

// SetRunning records in the lease metadata that the holder is working,
// optionally with an open run, so another process that finds the device
// held can say why. An empty runID means no run is open.
func (s *Session) SetRunning(runID string) error {
	return s.updateHolder(func(h *lease.Holder) {
		h.IdleSince = nil
		h.RunID = runID
	})
}

// SetIdle records in the lease metadata that the holder has gone idle
// at the current time.
func (s *Session) SetIdle() error {
	now := time.UnixMilli(s.deps.Clock.NowMs()).UTC()
	return s.updateHolder(func(h *lease.Holder) {
		h.IdleSince = &now
		h.RunID = ""
	})
}

func (s *Session) updateHolder(mutate func(*lease.Holder)) error {
	if s.lease == nil {
		return nil
	}
	err := s.lease.UpdateHolder(mutate)
	if errors.Is(err, lease.ErrReleased) {
		return nil
	}
	return err
}

// Close ends the session: it stops the lease watcher, disconnects the
// driver, which stops a runner the factory launched, and releases the
// lease. The disconnect is bounded by the shared shutdown grace
// period. Close is idempotent and returns the first call's result.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.watchCancel()
		s.watchers.Wait()

		state := s.liveLaunchState()
		s.mu.Lock()
		s.saved = &state
		s.mu.Unlock()

		disconnected := make(chan error, 1)
		var disconnectErr error
		if shutdown.Bounded(s.deps.Clock, shutdown.GracePeriod, func() { disconnected <- s.built.Driver.Disconnect() }) {
			disconnectErr = <-disconnected
		} else {
			disconnectErr = ErrDisconnectTimeout
		}
		var releaseErr error
		if s.lease != nil {
			releaseErr = s.lease.Release()
		}
		s.closeErr = errors.Join(disconnectErr, releaseErr)
		close(s.closed)
	})
	return s.closeErr
}
