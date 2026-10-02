package shutdown

import (
	"sync/atomic"
	"testing"
	"time"
)

const testGrace = 30 * time.Millisecond

func TestBoundedReportsCompletion(t *testing.T) {
	ran := false
	if !Bounded(time.Second, func() { ran = true }) {
		t.Fatal("Bounded returned false for a cleanup that finished")
	}
	if !ran {
		t.Fatal("cleanup did not run")
	}
}

func TestBoundedGivesUpOnAHungCleanup(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	if Bounded(testGrace, func() { <-release }) {
		t.Fatal("Bounded returned true for a cleanup that never finished")
	}
}

func TestForceExitAfterFiresWhenNotCanceled(t *testing.T) {
	fired := make(chan struct{})
	ForceExitAfter(testGrace, func() { close(fired) })
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog never fired")
	}
}

func TestForceExitAfterCanBeDisarmed(t *testing.T) {
	var fired atomic.Bool
	cancel := ForceExitAfter(testGrace, func() { fired.Store(true) })
	cancel()
	cancel()
	time.Sleep(4 * testGrace)
	if fired.Load() {
		t.Fatal("watchdog fired after cancel")
	}
}
