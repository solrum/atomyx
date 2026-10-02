package session

import (
	"context"

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
