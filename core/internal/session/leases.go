package session

import (
	"context"

	"dev.atomyx/internal/lease"
)

// LeaseAcquirer is the part of the lease manager that Open uses: it
// takes the exclusive lease on one device, and names the directory the
// per-device runner state files live next to. *lease.Manager satisfies
// it, so a host passes its manager and a test passes a substitute.
type LeaseAcquirer interface {
	Acquire(ctx context.Context, key lease.Key, holder lease.Holder) (*lease.Lease, error)
	Dir() string
}

// SocketNamer is implemented by a LeaseAcquirer that also says where the
// holder of a device serves viewers. *lease.Manager satisfies it. Open
// binds no attach socket for an acquirer that does not.
type SocketNamer interface {
	SocketPath(key lease.Key) string
}
