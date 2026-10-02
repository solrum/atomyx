package mcp

import (
	"context"
	"sync"
)

// scriptFlight tracks the script executions running against one run, so
// whoever ends the run can cancel them and wait until each has returned
// before the run is finalized. It is safe for concurrent use.
type scriptFlight struct {
	mu      sync.Mutex
	running map[*flightEntry]struct{}
}

type flightEntry struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func newScriptFlight() *scriptFlight {
	return &scriptFlight{running: map[*flightEntry]struct{}{}}
}

// begin registers one script execution. The returned context ends when
// parent ends or when cancelAndWait runs. The returned function must be
// called exactly once, after the execution has made its last write into
// the run.
func (f *scriptFlight) begin(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	entry := &flightEntry{cancel: cancel, done: make(chan struct{})}
	f.mu.Lock()
	f.running[entry] = struct{}{}
	f.mu.Unlock()
	return ctx, func() {
		cancel()
		f.mu.Lock()
		delete(f.running, entry)
		f.mu.Unlock()
		close(entry.done)
	}
}

// cancelAndWait cancels every registered execution and waits until each
// has called its end function. It returns ctx's error when ctx ends
// first, and the executions it did not wait for keep running.
func (f *scriptFlight) cancelAndWait(ctx context.Context) error {
	f.mu.Lock()
	entries := make([]*flightEntry, 0, len(f.running))
	for entry := range f.running {
		entries = append(entries, entry)
	}
	f.mu.Unlock()
	for _, entry := range entries {
		entry.cancel()
	}
	for _, entry := range entries {
		select {
		case <-entry.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
