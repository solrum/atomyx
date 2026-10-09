package session

import (
	"context"
	"time"

	"dev.atomyx/internal/logging"
)

// pauseCaptureBudget bounds one capture taken for a pause or after an
// act. It is separate from the act's own budget, so a slow device read
// cannot turn a finished act into a failure and cannot hold the debug
// lock for long.
const pauseCaptureBudget = 10 * time.Second

// captureHeld refreshes the hub after a device-driving act. The caller
// holds the debug lock, which keeps the capture from overlapping another
// act or a resume. A failure is logged and otherwise ignored, because the
// act already happened and its result stands.
func (s *Session) captureHeld(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	if _, paused := s.Paused(); !paused {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, pauseCaptureBudget)
	defer cancel()
	s.captureLogged(ctx)
}

// captureOnPause refreshes the hub when the run stops, so a viewer that
// attaches to the paused run sees the current screen without asking for a
// capture. It takes the debug lock and gives up when the run leaves the
// pause, the session closes or the budget runs out.
func (s *Session) captureOnPause() {
	ctx, cancel := context.WithTimeout(context.Background(), pauseCaptureBudget)
	defer cancel()
	go func() {
		select {
		case <-s.closed:
			cancel()
		case <-ctx.Done():
		}
	}()
	release, err := s.lockDebug(ctx)
	if err != nil {
		return
	}
	defer release()
	if _, paused := s.Paused(); !paused {
		return
	}
	s.captureLogged(ctx)
}

func (s *Session) captureLogged(ctx context.Context) {
	if err := s.captureDevice(ctx); err != nil {
		s.deps.Logger.Warn("could not capture the screen of the paused run", logging.LogContext{"id": s.desc.ID, "error": err.Error()})
	}
}
