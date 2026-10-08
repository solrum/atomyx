package mcp

import (
	"context"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
)

var _ session.Yielder = (*DeviceSession)(nil)

// Yield implements session.Yielder. It accepts only while the held
// session is idle, with no tool call or capture inside the activity gate
// and no run open, because a yield in the middle of work would take the
// device away from it.
//
// Accepting detaches the session under s.mu, which is what every other
// path that closes it checks first, so the idle timer, a disconnect and
// the loss watcher find nothing left to close. The exclusive gate stays
// held until the close ends, so a call that arrives meanwhile waits and
// then sees a device it must take again. Yield never takes acquireMu,
// because the idle release holds it while it closes the session, and that
// close waits for this connection's goroutine.
func (s *DeviceSession) Yield(ctx context.Context, borrower string) (func(), error) {
	endExclusive, ok := s.gate.TryExclusive()
	if !ok {
		return nil, diag.NewHolderBusy("a tool call or a capture is running")
	}
	s.mu.Lock()
	if busy := s.yieldRefusalLocked(); busy != "" {
		s.mu.Unlock()
		endExclusive()
		return nil, diag.NewHolderBusy(busy)
	}
	sess, remembered := s.sess, s.remembered
	s.sess, s.active = nil, nil
	s.state = holderBorrowed
	s.cancelTimerLocked()
	s.borrowGen++
	gen := s.borrowGen
	viewCtx, stopView := context.WithCancel(context.WithoutCancel(ctx))
	s.borrowStop = stopView
	s.yields.Add(1)
	s.mu.Unlock()

	sess.RecordIntervention(session.InterventionControlBorrowed, borrower)
	return func() {
		defer s.yields.Done()
		s.closeAndRemember(context.WithoutCancel(viewCtx), sess, remembered, "session.borrowed")
		endExclusive()
		s.followBorrower(viewCtx, gen, sess.Descriptor())
	}, nil
}

// yieldRefusalLocked names what keeps the session from yielding, or is
// empty when it may.
func (s *DeviceSession) yieldRefusalLocked() string {
	switch {
	case s.closed || s.sess == nil:
		return "the session holds no device at the moment"
	case s.pausedLocked():
		return "a person is debugging a run on this device"
	case s.inFlight > 0:
		return "a tool call is running"
	case s.openRunID() != "":
		return "a run is open"
	case s.state != holderIdle:
		return "the session is " + string(s.state)
	}
	return ""
}

// followBorrower watches the process that took the device, and moves the
// session to released when that process ends, when none took the device
// within session.FollowBudgetMs, or when it cannot be watched. A
// selection or a re-acquire that installs a session first ends the watch
// through ctx and keeps the state it set.
func (s *DeviceSession) followBorrower(ctx context.Context, gen uint64, desc device.Descriptor) {
	defer s.endBorrow(gen)
	prober, ok := s.deps.Leases.(session.LeaseProber)
	if !ok {
		return
	}
	handle, err := session.Follow(ctx, session.AttachDeps{
		Leases: prober, Identity: s.deps.Identity, Clock: s.deps.Clock, Client: s.deps.Command,
	}, desc)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Info("session.borrower_not_followed", logging.LogContext{"id": desc.ID, "reason": err.Error()})
		}
		return
	}
	defer func() { _ = handle.Close() }()
	if holder := handle.Info().Holder; holder.PID != 0 { //nolint:contextcheck // the handle bounds its status call with its own timeout
		s.rememberBorrower(gen, holder.Describe())
	}
	select {
	case <-handle.Done():
	case <-ctx.Done():
	}
}

// rememberBorrower stores the description of the watched borrower while
// the borrow numbered gen is still the current one.
func (s *DeviceSession) rememberBorrower(gen uint64, holder string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == holderBorrowed && s.borrowGen == gen {
		s.borrowerHolder = holder
	}
}

// borrowerLabelLocked names the process that took the device, falling back
// to a generic phrase until it is watched or when it cannot be identified.
func (s *DeviceSession) borrowerLabelLocked() string {
	if s.borrowerHolder == "" {
		return "the process that took the device over"
	}
	return s.borrowerHolder
}

// endBorrow moves a session that is still in the borrow numbered gen to
// released.
func (s *DeviceSession) endBorrow(gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == holderBorrowed && s.borrowGen == gen {
		s.state = holderReleased
		s.releasedBy = releasedHandedOver
		s.stopBorrowViewLocked()
	}
}

// stopBorrowViewLocked ends the watch of the device's borrower, if any.
func (s *DeviceSession) stopBorrowViewLocked() {
	s.borrowerHolder = ""
	if s.borrowStop != nil {
		s.borrowStop()
		s.borrowStop = nil
	}
}
