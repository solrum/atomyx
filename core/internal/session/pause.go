package session

// Paused reports where the run is paused, or false when it is not. Only a
// host that injected a Debugger has a reason to pause its session.
func (s *Session) Paused() (PauseLocation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pause == nil {
		return PauseLocation{}, false
	}
	return *s.pause, true
}

// EnterPause marks the run as stopped at loc and tells the viewers. The
// host's step gate calls it when the runner blocks, and calls it again
// with the new location for each later stop. SetRunning, SetIdle and the
// host's own activity bookkeeping cannot clear the mark.
func (s *Session) EnterPause(loc PauseLocation) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	s.mu.Lock()
	s.pause = &loc
	s.mu.Unlock()
	holder := s.Info().Holder
	s.hub.PublishState(StatePaused, holder.Command, holder.RunID)
}

// LeavePause clears the pause mark and tells the viewers the activity
// the session returns to. It is a no-op when the run is not paused.
func (s *Session) LeavePause() {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	s.mu.Lock()
	was := s.pause != nil
	s.pause = nil
	s.mu.Unlock()
	if !was {
		return
	}
	info := s.Info()
	s.hub.PublishState(info.State, info.Holder.Command, info.Holder.RunID)
}
