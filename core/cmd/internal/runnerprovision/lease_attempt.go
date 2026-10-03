package runnerprovision

import (
	"context"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
)

// cleanupHolderCommand and cleanupHolderRole label the lease a keeper
// holds for the moment it stops a runner.
const (
	cleanupHolderCommand = "atomyx"
	cleanupHolderRole    = "runner-cleanup"
)

// leaseTaker is the part of a lease manager the keeper's lease attempt
// needs.
type leaseTaker interface {
	TryAcquire(ctx context.Context, key lease.Key, holder lease.Holder) (*lease.Lease, error)
}

// leaseAttempt returns the keeper's lease attempt for an iOS device. It
// makes one non-blocking attempt on the lease the session layer holds
// for the same device, so a lease held by any process, including a
// session of this one, is reported as an error.
func leaseAttempt(taker leaseTaker, pid int, command string) device.LeaseAttempt {
	return func(udid string) (func(), error) {
		held, err := taker.TryAcquire(context.Background(), lease.KeyFor(string(device.PlatformIOS), udid), lease.Holder{
			PID: pid, Command: command, Role: cleanupHolderRole, StartedAt: time.Now().UTC(),
		})
		if err != nil {
			return nil, err
		}
		return func() { _ = held.Release() }, nil
	}
}
