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
	return Info{Device: s.desc, Holder: holder, State: s.holderState()}
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

// FreshTree implements Controller.
func (s *Session) FreshTree(ctx context.Context) (Snapshot, error) {
	return s.FreshTreeIf(ctx, admitAlways)
}

func admitAlways() error { return nil }

// FreshTreeIf implements Refresher.
func (s *Session) FreshTreeIf(ctx context.Context, admit func() error) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	release, ok := s.gate.TryExclusive()
	if !ok {
		return Snapshot{}, diag.NewHolderBusy("a tool call or another fresh capture is running")
	}
	defer release()
	select {
	case <-s.closed:
		return Snapshot{}, ErrClosed
	default:
	}
	if err := admit(); err != nil {
		return Snapshot{}, err
	}

	o := s.built.Orchestra
	if _, err := o.Hierarchy(ctx); err != nil {
		return Snapshot{}, err
	}
	if _, err := o.Screenshot(ctx); err != nil {
		return Snapshot{}, err
	}
	// The screen size only changes with the screen, so one reading per
	// action epoch serves every later capture in it.
	if !s.hub.sizeCurrent() {
		if _, err := o.ScreenSize(ctx); err != nil {
			return Snapshot{}, err
		}
	}
	return s.hub.Latest(), nil
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
