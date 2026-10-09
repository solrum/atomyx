package session

import (
	"context"
	"errors"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/runfolder"
)

var (
	_ Viewer     = (*Session)(nil)
	_ StepSource = (*Session)(nil)
)

// Info implements Viewer.
func (s *Session) Info() Info {
	holder := lease.Holder{PID: s.deps.PID, Command: s.deps.Command, Role: s.deps.Role}
	if s.lease != nil {
		holder = s.lease.Holder()
	}
	info := Info{Device: s.desc, Holder: holder, State: s.holderState()}
	if loc, paused := s.Paused(); paused && info.State == StatePaused {
		info.Pause = &loc
	}
	return info
}

// holderState reports the session's state. A closed session wins over a
// lost one, because closing is the owner's last word.
func (s *Session) holderState() HolderState {
	select {
	case <-s.closed:
		return StateClosed
	default:
	}
	select {
	case <-s.lost:
		return StateLost
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pause != nil {
		return StatePaused
	}
	return s.activity
}

// Latest implements Viewer.
func (s *Session) Latest() Snapshot { return s.hub.Latest() }

// Subscribe implements Viewer.
func (s *Session) Subscribe(ctx context.Context) (<-chan Observation, error) {
	return s.hub.Subscribe(ctx)
}

var (
	_ Controller = (*Session)(nil)
	_ Refresher  = (*Session)(nil)
)

// ErrClosed reports a request made of a session that has been closed.
var ErrClosed = errors.New("the session is closed")

// Gate reports the activity gate that FreshTree takes. A host that runs
// tool calls enters it around every call that drives the device.
func (s *Session) Gate() *ActivityGate { return s.gate }

// FreshTree implements Refresher.
func (s *Session) FreshTree(ctx context.Context) (Snapshot, error) {
	return s.FreshTreeIf(ctx, admitAlways)
}

func admitAlways() error { return nil }

// FreshTreeIf behaves like FreshTree, except that admit runs after the
// exclusive activity gate is taken and before any driver call. A non-nil
// error from admit is returned unchanged and the device is not touched.
// admit runs under the gate, so it must not enter the gate itself, because
// the gate is not reentrant. A held gate still wins: the call returns a
// diag.CodeHolderBusy error without running admit.
//
// While the run is paused, the capture is serialized with act by the
// debug lock instead of the gate, because the paused run is itself inside
// the gate. A capture then fails with a diag.CodeHolderBusy error while an
// act or another capture holds the lock.
func (s *Session) FreshTreeIf(ctx context.Context, admit func() error) (Snapshot, error) {
	return s.freshTree(ctx, func(bool) error { return admit() })
}

// freshTree is FreshTreeIf with an admit that learns whether the capture
// runs under the debug lock.
func (s *Session) freshTree(ctx context.Context, admit func(underDebugLock bool) error) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	release, underDebugLock, ok := s.takeCaptureSlot()
	if !ok {
		return Snapshot{}, diag.NewHolderBusy("a tool call, an act or another fresh capture is running")
	}
	defer release()
	select {
	case <-s.closed:
		return Snapshot{}, ErrClosed
	default:
	}
	if err := admit(underDebugLock); err != nil {
		return Snapshot{}, err
	}

	if err := s.captureDevice(ctx); err != nil {
		return Snapshot{}, err
	}
	return s.hub.Latest(), nil
}

// captureDevice reads the tree, the screenshot and, once per action epoch,
// the screen size through the observing driver, so the hub holds the
// screen as it is now. The caller owns the right to drive the device: the
// activity gate or, while the run is paused, the debug lock.
func (s *Session) captureDevice(ctx context.Context) error {
	o := s.built.Orchestra
	if _, err := o.Hierarchy(ctx); err != nil {
		return err
	}
	if _, err := o.Screenshot(ctx); err != nil {
		return err
	}
	// The screen size only changes with the screen, so one reading per
	// action epoch serves every later capture in it.
	if !s.hub.sizeCurrent() {
		if _, err := o.ScreenSize(ctx); err != nil {
			return err
		}
	}
	return nil
}

// PublishStep hands a stamped step line to the session's viewers.
func (s *Session) PublishStep(line runfolder.StepLine) { s.hub.PublishStep(line) }

// StepsSince implements StepSource.
func (s *Session) StepsSince(runID string, sinceSeq int64) ([]runfolder.StepLine, bool, error) {
	return s.hub.StepsSince(runID, sinceSeq)
}

// SubscribeSince implements StepSource.
func (s *Session) SubscribeSince(ctx context.Context, runID string, sinceSeq int64) (<-chan Observation, error) {
	return s.hub.SubscribeSince(ctx, runID, sinceSeq)
}
