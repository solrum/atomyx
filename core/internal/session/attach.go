package session

import (
	"context"
	"fmt"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/lease"
)

const (
	// AttachBackoffInitialMs is the first wait between two probes of a
	// held device. Each later wait doubles, up to AttachBackoffMaxMs.
	AttachBackoffInitialMs int64 = 100
	// AttachBackoffMaxMs caps one wait between two probes.
	AttachBackoffMaxMs int64 = 2_000
	// AttachStartingBudgetMs is the total wait for a holder that has
	// the lease but has not yet published a socket or the reason it
	// serves none. It exceeds the budget of one device acquisition, so a
	// holder that is launching a runner is still waited for.
	AttachStartingBudgetMs int64 = 130_000
	// AttachReconnectBudgetMs is the total wait, after a failed connect,
	// for the holder to either answer or release the lease. A holder that
	// is exiting does both within this time.
	AttachReconnectBudgetMs int64 = 3_000
)

// LeaseProber is the part of the lease manager that Attach uses.
// *lease.Manager satisfies it.
type LeaseProber interface {
	Probe(ctx context.Context, key lease.Key) (lease.Status, error)
}

// AttachDeps configures Attach.
type AttachDeps struct {
	// Leases probes who holds a device. Required.
	Leases LeaseProber
	// Identity resolves the canonical identity the lease keys on. Nil
	// keys the lease on the descriptor's own platform and id, as for
	// Deps.Identity.
	Identity IdentityResolver
	// Clock times the waits between probes. Defaults to
	// clock.SystemClock.
	Clock clock.Clock
	// Client names the viewer in the hello request. Defaults to "atomyx".
	Client string
	// Dial connects to a holder's socket. Defaults to DialRemote.
	Dial func(ctx context.Context, cfg RemoteConfig, desc device.Descriptor) (*RemoteHandle, error)
}

// AttachResult is the outcome of an Attach that did not fail. Exactly one
// field is set.
type AttachResult struct {
	// Handle follows the holder. The caller closes it.
	Handle *RemoteHandle
	// Free reports that no process holds the device, so the caller may
	// acquire it instead of attaching.
	Free bool
}

// Attach connects to the process that holds the device named by desc.
//
// A free device returns AttachResult.Free. A holder that serves a socket
// is connected to. A holder that serves none fails with
// *HolderServesNoSocketError. A holder that has the lease but has not yet
// published its socket is waited for, up to AttachStartingBudgetMs, then
// fails with *HolderStartingError. A connect that fails is followed by new
// probes, because the holder may be exiting: a lease that became free
// returns AttachResult.Free, and a holder that still holds it after
// AttachReconnectBudgetMs fails with *HolderUnreachableError. All three
// errors carry DEVICE_HELD. A canceled ctx returns its own error.
func Attach(ctx context.Context, deps AttachDeps, desc device.Descriptor) (AttachResult, error) {
	deps = deps.withDefaults()
	identity, err := Deps{Identity: deps.Identity}.ResolveIdentity(ctx, desc)
	if err != nil {
		return AttachResult{}, err
	}
	key := lease.KeyFor(string(identity.Platform), identity.HardwareID)
	starting := backoff{budgetMs: AttachStartingBudgetMs}
	reconnecting := backoff{budgetMs: AttachReconnectBudgetMs}
	for {
		status, err := deps.Leases.Probe(ctx, key)
		if err != nil {
			return AttachResult{}, fmt.Errorf("probe the device lease: %w", err)
		}
		if !status.Held {
			return AttachResult{Free: true}, nil
		}
		holder := status.Holder
		switch {
		case status.HolderKnown && holder.Socket != "":
			handle, dialErr := deps.Dial(ctx, RemoteConfig{Path: holder.Socket, Client: deps.Client}, desc)
			if dialErr == nil {
				return AttachResult{Handle: handle}, nil
			}
			if err := ctx.Err(); err != nil {
				return AttachResult{}, err
			}
			more, err := reconnecting.wait(ctx, deps.Clock)
			if err != nil {
				return AttachResult{}, err
			}
			if !more {
				return AttachResult{}, newHolderUnreachable(holder, dialErr)
			}
		case status.HolderKnown && holder.SocketError != "":
			return AttachResult{}, newHolderServesNoSocket(holder)
		default:
			more, err := starting.wait(ctx, deps.Clock)
			if err != nil {
				return AttachResult{}, err
			}
			if !more {
				return AttachResult{}, newHolderStarting(holder)
			}
		}
	}
}

func (d AttachDeps) withDefaults() AttachDeps {
	if d.Clock == nil {
		d.Clock = clock.SystemClock{}
	}
	if d.Client == "" {
		d.Client = "atomyx"
	}
	if d.Dial == nil {
		d.Dial = DialRemote
	}
	return d
}

// backoff spends a fixed total wait in doubling steps.
type backoff struct {
	budgetMs int64
	spentMs  int64
	nextMs   int64
}

// wait sleeps one step and reports whether budget remains for more. It
// reports false without sleeping once the budget is spent.
func (b *backoff) wait(ctx context.Context, clk clock.Clock) (bool, error) {
	if b.spentMs >= b.budgetMs {
		return false, nil
	}
	if b.nextMs == 0 {
		b.nextMs = AttachBackoffInitialMs
	}
	step := min(b.nextMs, AttachBackoffMaxMs, b.budgetMs-b.spentMs)
	if err := clk.Sleep(ctx, step); err != nil {
		return false, err
	}
	b.spentMs += step
	b.nextMs = min(b.nextMs*2, AttachBackoffMaxMs)
	return true, nil
}

const stopHolderRemedy = "Stop that process or wait for it to finish, then run the command again."

// HolderServesNoSocketError reports a held device whose holder serves no
// attach socket, so no viewer can watch it.
type HolderServesNoSocketError struct {
	Holder lease.Holder
	cause  error
}

func newHolderServesNoSocket(h lease.Holder) *HolderServesNoSocketError {
	return &HolderServesNoSocketError{Holder: h, cause: diag.New(diag.CodeDeviceHeld,
		fmt.Sprintf("the process holding the device serves no attach socket: %s", h.Describe()),
		stopHolderRemedy, nil)}
}

func (e *HolderServesNoSocketError) Error() string { return e.cause.Error() }
func (e *HolderServesNoSocketError) Unwrap() error { return e.cause }

// HolderStartingError reports a held device whose holder has not yet
// published a socket within AttachStartingBudgetMs.
type HolderStartingError struct {
	Holder lease.Holder
	cause  error
}

func newHolderStarting(h lease.Holder) *HolderStartingError {
	return &HolderStartingError{Holder: h, cause: diag.New(diag.CodeDeviceHeld,
		fmt.Sprintf("the process holding the device is still starting: %s", h.Describe()),
		"Try again in a moment, because the holder may still be launching its runner.", nil)}
}

func (e *HolderStartingError) Error() string { return e.cause.Error() }
func (e *HolderStartingError) Unwrap() error { return e.cause }

// HolderUnreachableError reports a held device whose holder published a
// socket that could not be connected to within AttachReconnectBudgetMs.
type HolderUnreachableError struct {
	Holder lease.Holder
	cause  error
}

func newHolderUnreachable(h lease.Holder, dialErr error) *HolderUnreachableError {
	return &HolderUnreachableError{Holder: h, cause: diag.New(diag.CodeDeviceHeld,
		fmt.Sprintf("the process holding the device did not accept a connection: %s", h.Describe()),
		stopHolderRemedy, dialErr)}
}

func (e *HolderUnreachableError) Error() string { return e.cause.Error() }
func (e *HolderUnreachableError) Unwrap() error { return e.cause }
