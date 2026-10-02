package session

import (
	"context"
	"errors"

	"dev.atomyx/internal/lease"
)

var _ Viewer = (*Session)(nil)

// Info implements Viewer.
func (s *Session) Info() Info {
	holder := lease.Holder{PID: s.deps.PID, Command: s.deps.Command, Role: s.deps.Role}
	if s.lease != nil {
		holder = s.lease.Holder()
	}
	state := holder.State()
	select {
	case <-s.closed:
		state = "closed"
	case <-s.lost:
		state = "lost"
	default:
	}
	return Info{Device: s.desc, Holder: holder, Role: s.deps.Role, State: state}
}

// Latest implements Viewer.
func (s *Session) Latest() Snapshot { return s.hub.Latest() }

// Subscribe implements Viewer.
func (s *Session) Subscribe(ctx context.Context) (<-chan Observation, error) {
	return s.hub.Subscribe(ctx)
}

var _ Controller = (*Session)(nil)

// ErrClosed reports a request made of a session that has been closed.
var ErrClosed = errors.New("the session is closed")

// Gate reports the activity gate that FreshTree takes. A host that runs
// tool calls enters it around every call that drives the device.
func (s *Session) Gate() *ActivityGate { return s.gate }

// FreshTree implements Controller.
func (s *Session) FreshTree(ctx context.Context) (Snapshot, error) {
	release, err := s.gate.Exclusive(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	select {
	case <-s.closed:
		return Snapshot{}, ErrClosed
	default:
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
