package session

import (
	"context"

	"dev.atomyx/internal/diag"
)

// tryDebugLock takes the debug lock when it is free.
func (s *Session) tryDebugLock() (release func(), ok bool) {
	select {
	case s.debugLock <- struct{}{}:
		return s.releaseDebugLock, true
	default:
		return nil, false
	}
}

// lockDebug waits for the debug lock until ctx ends.
func (s *Session) lockDebug(ctx context.Context) (release func(), err error) {
	select {
	case s.debugLock <- struct{}{}:
		return s.releaseDebugLock, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Session) lockDebugUninterruptibly() (release func()) {
	s.debugLock <- struct{}{}
	return s.releaseDebugLock
}

func (s *Session) releaseDebugLock() { <-s.debugLock }

// takeCaptureSlot admits a fresh capture. While the run is paused it
// takes the debug lock, because the paused run is inside the activity
// gate and the capture must exclude only an act. Otherwise it takes the
// gate. The lock is rechecked under the lock itself, so a capture never
// holds it after the run left the pause.
func (s *Session) takeCaptureSlot() (release func(), underDebugLock, ok bool) {
	if _, paused := s.Paused(); paused {
		release, locked := s.tryDebugLock()
		if !locked {
			return nil, false, false
		}
		if _, still := s.Paused(); still {
			return release, true, true
		}
		release()
	}
	release, ok = s.gate.TryExclusive()
	return release, false, ok
}

func notPaused() error { return diag.NewHolderBusy("the run is not paused") }

// Act runs one step on the device while the run is paused. It waits for
// the debug lock, so it never overlaps a fresh capture or another act.
// It fails with diag.CodeUnsupported when the session has no Debugger and
// with diag.CodeHolderBusy when the run is not paused.
func (s *Session) Act(ctx context.Context, stepYAML string) (ActResult, error) {
	if s.debugger == nil {
		return ActResult{}, diag.NewUnsupported("act.step")
	}
	release, err := s.lockDebug(ctx)
	if err != nil {
		return ActResult{}, err
	}
	defer release()
	if _, paused := s.Paused(); !paused {
		return ActResult{}, notPaused()
	}
	return s.debugger.Act(ctx, stepYAML)
}

// Resume lets the paused run continue. It waits for an act in flight,
// because the runner must not drive the device while an act does.
//
// The pause mark is cleared before the Debugger wakes the runner, so a
// runner that stops at its very next step is never overwritten by a late
// clear. A refusal or an error puts the mark back and publishes nothing.
// It fails with diag.CodeUnsupported when the session has no Debugger and
// with diag.CodeHolderBusy when the run is not paused.
func (s *Session) Resume(ctx context.Context, opts ResumeOptions) (ResumeResult, error) {
	if s.debugger == nil {
		return ResumeResult{}, diag.NewUnsupported("debug.resume")
	}
	release, err := s.lockDebug(ctx)
	if err != nil {
		return ResumeResult{}, err
	}
	defer release()
	loc, paused := s.clearPause()
	if !paused {
		return ResumeResult{}, notPaused()
	}
	result, err := s.debugger.Resume(ctx, opts)
	if err != nil || !result.Resumed {
		s.mu.Lock()
		s.pause = &loc
		s.mu.Unlock()
		return result, err
	}
	s.publishCurrent()
	return result, nil
}

// SetBreakpoints replaces every breakpoint of the run. It is admitted at
// any time, and fails with diag.CodeUnsupported when the session has no
// Debugger.
func (s *Session) SetBreakpoints(locations []string) error {
	if s.debugger == nil {
		return diag.NewUnsupported("debug.breakpoints.set")
	}
	return s.debugger.SetBreakpoints(locations)
}

// Pause asks the run to stop before its next step. It is admitted at any
// time, and fails with diag.CodeUnsupported when the session has no
// Debugger.
func (s *Session) Pause() error {
	if s.debugger == nil {
		return diag.NewUnsupported("debug.pause")
	}
	return s.debugger.Pause()
}

// Variables reports the variables visible at the pause, already redacted.
// It fails with diag.CodeUnsupported when the session has no Debugger and
// with diag.CodeHolderBusy when the run is not paused.
func (s *Session) Variables() (VariablesView, error) {
	if s.debugger == nil {
		return VariablesView{}, diag.NewUnsupported("debug.variables.get")
	}
	if _, paused := s.Paused(); !paused {
		return VariablesView{}, notPaused()
	}
	return s.debugger.Variables()
}
