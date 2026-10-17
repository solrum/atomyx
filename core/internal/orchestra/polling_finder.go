package orchestra

import (
	"context"
	"fmt"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/wire"
)

// HierarchyDriver is the narrow driver.Driver subset Finder needs: it
// does not tap, scroll, or otherwise mutate device state.
type HierarchyDriver interface {
	Hierarchy(ctx context.Context) (wire.TreeNodeWire, error)
}

// WaitOptions configures Finder.WaitFor.
type WaitOptions struct {
	// TimeoutMs is the maximum time to wait for filter to produce a
	// match.
	TimeoutMs int
	// PollIntervalMs is the delay between successive hierarchy
	// polls. Zero selects the default of 250ms.
	PollIntervalMs int
}

// FindTimeoutError is returned by Finder.WaitFor when the deadline
// elapses before filter produces a match.
type FindTimeoutError struct {
	ElapsedMs int64
	PollCount int
	// LastReadErr is the transient hierarchy read error of the final
	// poll, or nil when the final poll read the hierarchy and found no
	// match.
	LastReadErr error
}

func (e *FindTimeoutError) Error() string {
	msg := fmt.Sprintf("filter produced no matches after %dms (polled %d times)", e.ElapsedMs, e.PollCount)
	if e.LastReadErr != nil {
		msg += fmt.Sprintf(", last hierarchy read failed: %v", e.LastReadErr)
	}
	return msg
}

// Unwrap exposes the last transient read error to errors.Is and errors.As.
func (e *FindTimeoutError) Unwrap() error { return e.LastReadErr }

// Code implements diag.Coded.
func (e *FindTimeoutError) Code() string { return diag.CodeToolTimeout }

// Remedy implements diag.Coded.
func (e *FindTimeoutError) Remedy() string {
	return "Re-orient with a fresh read of the screen, then retry the action."
}

var _ diag.Coded = (*FindTimeoutError)(nil)

// Finder is the bridge between the pure selector-resolution engine
// (ElementFilter, CompileSelector) and a live driver. Find captures
// one Driver.Hierarchy snapshot and runs the filter against it;
// WaitFor polls on a Clock-driven interval until the filter yields a
// non-empty result or its budget expires.
type Finder struct {
	driver HierarchyDriver
	clock  Clock
}

// NewFinder constructs a Finder over driver and clock.
func NewFinder(driverDep HierarchyDriver, clock Clock) *Finder {
	return &Finder{driver: driverDep, clock: clock}
}

// Find captures the current hierarchy and returns every cursor filter
// matches, in the order filter produces them.
func (f *Finder) Find(ctx context.Context, filter ElementFilter) ([]*TreeCursor, error) {
	tree, err := f.driver.Hierarchy(ctx)
	if err != nil {
		return nil, err
	}
	return filter(FromTree(tree)), nil
}

// FindOne captures the current hierarchy and returns filter's first
// match, or nil when there is none. It never returns FindTimeoutError.
func (f *Finder) FindOne(ctx context.Context, filter ElementFilter) (*TreeCursor, error) {
	results, err := f.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

// WaitFor repeatedly captures the hierarchy and runs filter until at
// least one match appears or opts.TimeoutMs elapses, returning
// FindTimeoutError on timeout.
//
// A hierarchy read that fails with a transient agent error (see
// diag.IsTransient) counts as "no match yet" and polling continues.
// If the deadline passes after such a failure, FindTimeoutError carries
// that error in LastReadErr. Any other read error ends the wait at once.
//
// Timing: the first poll fires immediately, with no upfront sleep.
// Subsequent polls wait opts.PollIntervalMs between attempts. The
// budget is enforced on a per-poll deadline check, not by racing a
// timer against the poll call, so a slow hierarchy call completes
// honestly rather than being cancelled mid-flight.
func (f *Finder) WaitFor(ctx context.Context, filter ElementFilter, opts WaitOptions) ([]*TreeCursor, error) {
	pollIntervalMs := int64(opts.PollIntervalMs)
	if pollIntervalMs <= 0 {
		pollIntervalMs = 250
	}
	startedAt := f.clock.NowMs()
	deadline := startedAt + int64(opts.TimeoutMs)
	polls := 0
	var lastReadErr error
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		polls++
		results, err := f.Find(ctx, filter)
		if err != nil && !diag.IsTransient(err) {
			return nil, err
		}
		lastReadErr = err
		if len(results) > 0 {
			return results, nil
		}
		now := f.clock.NowMs()
		if now >= deadline {
			return nil, &FindTimeoutError{ElapsedMs: now - startedAt, PollCount: polls, LastReadErr: lastReadErr}
		}
		sleepMs := pollIntervalMs
		if remaining := deadline - now; remaining < sleepMs {
			sleepMs = remaining
		}
		if err := f.clock.Sleep(ctx, sleepMs); err != nil {
			return nil, err
		}
	}
}
