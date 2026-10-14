//go:build e2e

package iosdriver_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

// exitGrace is how long a test waits for a process to be observed as
// exited after the call that should have stopped it returned.
const exitGrace = 2 * time.Second

// trackedProcess is a started background process plus a channel that
// closes once the process has exited.
type trackedProcess struct {
	process.BackgroundProcess
	command string
	exited  chan struct{}
}

// recordingBackground starts every process through the real runner and
// remembers each one, so a test can check afterward which are still
// running.
type recordingBackground struct {
	inner process.BackgroundRunner

	mu      sync.Mutex
	started []*trackedProcess
}

func (r *recordingBackground) Start(ctx context.Context, name string, args ...string) (process.BackgroundProcess, error) {
	proc, err := r.inner.Start(ctx, name, args...)
	if err != nil {
		return nil, err
	}
	tracked := &trackedProcess{BackgroundProcess: proc, command: name, exited: make(chan struct{})}
	go func() {
		_, _ = proc.Wait()
		close(tracked.exited)
	}()
	r.mu.Lock()
	r.started = append(r.started, tracked)
	r.mu.Unlock()
	return tracked, nil
}

func (r *recordingBackground) processes() []*trackedProcess {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*trackedProcess(nil), r.started...)
}

// requireAllExited fails the test for every process r started that has
// not exited within exitGrace.
func (r *recordingBackground) requireAllExited(t *testing.T) {
	t.Helper()
	for _, proc := range r.processes() {
		select {
		case <-proc.exited:
		case <-time.After(exitGrace):
			t.Errorf("background process %q (pid %d) is still running", proc.command, proc.Pid())
		}
	}
}
