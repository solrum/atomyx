package session_test

import (
	"errors"
	"testing"
	"time"

	"dev.atomyx/internal/session/sessiontest"
)

func TestViewerWaitForLines_ReturnsAnErrorWhenTheHolderConnectionEnds(t *testing.T) {
	s, _, path := socketHolder(t)
	viewer := sessiontest.DialViewer(t, path, "waiter")
	viewer.FollowSteps(t)

	result := make(chan error, 1)
	go func() { result <- viewer.WaitForLines(1) }()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, sessiontest.ErrViewerEnded) {
			t.Fatalf("WaitForLines() error = %v, want ErrViewerEnded", err)
		}
	case <-time.After(socketWaitGuard):
		t.Fatalf("WaitForLines did not return within %s after the holder closed", socketWaitGuard)
	}
}
