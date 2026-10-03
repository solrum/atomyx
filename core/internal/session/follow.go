package session

import (
	"context"
	"errors"
	"sync/atomic"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
)

const (
	// FollowBudgetMs is the total wait for another process to take a
	// device this host just gave away. It covers the borrower's
	// acquisition after the holder's close.
	FollowBudgetMs int64 = 5_000
	// FollowIntervalMs is the wait between two probes of a device that is
	// still free.
	FollowIntervalMs int64 = 100
)

// ErrNoBorrower reports that no process took the device within
// FollowBudgetMs.
var ErrNoBorrower = errors.New("no process took the device")

// ErrBorrowerEnded reports that a process held the device while Follow
// waited and then released it before a watch could be set up.
var ErrBorrowerEnded = errors.New("the process that took the device ended")

var errFollowBudget = errors.New("the wait for the device's new holder is over")

// Follow attaches to the process that takes over desc right after this
// host gave it away. A device that is still free is probed again every
// FollowIntervalMs, because the borrower acquires it a moment after the
// holder's close. It fails with ErrNoBorrower when the budget runs out
// first, with ErrBorrowerEnded when another process held the device
// and then released it without being watched, with the error Attach
// reports when the new holder cannot be watched, and with ctx's own
// error when ctx ends. The caller closes the returned handle.
func Follow(ctx context.Context, deps AttachDeps, desc device.Descriptor) (*RemoteHandle, error) {
	deps = deps.withDefaults()
	seen := &heldRecorder{LeaseProber: deps.Leases}
	deps.Leases = seen
	bounded, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Canceled)
	go func() {
		if deps.Clock.Sleep(bounded, FollowBudgetMs) == nil {
			cancel(errFollowBudget)
		}
	}()
	for {
		result, err := Attach(bounded, deps, desc)
		if err == nil && result.Handle != nil {
			return result.Handle, nil
		}
		if err == nil && seen.held.Load() {
			return nil, ErrBorrowerEnded
		}
		if err == nil {
			err = deps.Clock.Sleep(bounded, FollowIntervalMs)
		}
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(context.Cause(bounded), errFollowBudget) {
			return nil, ErrNoBorrower
		}
		return nil, err
	}
}

// heldRecorder remembers whether any probe found the lease held. The host
// that follows gave its own lease away first, so a held lease belongs to
// another process.
type heldRecorder struct {
	LeaseProber
	held atomic.Bool
}

func (r *heldRecorder) Probe(ctx context.Context, key lease.Key) (lease.Status, error) {
	status, err := r.LeaseProber.Probe(ctx, key)
	if err == nil && status.Held {
		r.held.Store(true)
	}
	return status, err
}
