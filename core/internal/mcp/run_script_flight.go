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
	cancel context.CancelCauseFunc
	done   chan struct{}
}

func newScriptFlight() *scriptFlight {
	return &scriptFlight{running: map[*flightEntry]struct{}{}}
}

// begin registers one script execution. The returned context ends when
// parent ends or when cancelAndWait or cancelAll runs. The returned function must be
// called exactly once, after the execution has made its last write into
// the run.
func (f *scriptFlight) begin(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	entry := &flightEntry{cancel: cancel, done: make(chan struct{})}
	f.mu.Lock()
	f.running[entry] = struct{}{}
	f.mu.Unlock()
	return ctx, func() {
		cancel(nil)
		f.mu.Lock()
		delete(f.running, entry)
		f.mu.Unlock()
		close(entry.done)
	}
}

// cancelAll cancels every registered execution with cause, which
// context.Cause reports on each execution's context, and does not wait.
// A cause that is nil reads as context.Canceled.
func (f *scriptFlight) cancelAll(cause error) {
	for _, entry := range f.entries() {
		entry.cancel(cause)
	}
}

func (f *scriptFlight) entries() []*flightEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries := make([]*flightEntry, 0, len(f.running))
	for entry := range f.running {
		entries = append(entries, entry)
	}
	return entries
}

// cancelAndWait cancels every registered execution and waits until each
// has called its end function. It returns ctx's error when ctx ends
// first, and the executions it did not wait for keep running.
func (f *scriptFlight) cancelAndWait(ctx context.Context) error {
	entries := f.entries()
	for _, entry := range entries {
		entry.cancel(nil)
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
