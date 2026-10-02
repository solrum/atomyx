// Package sessiontest holds helpers that only tests use to exercise a
// session host. Production code never imports it, which
// scripts/check-sessiontest-imports.sh enforces.
package sessiontest

import (
	"context"
	"os"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/lease"
)

// Leases is a session.LeaseAcquirer over a real lease directory, with a
// clock a test steps by hand, so a host's tests exercise exclusion
// between several sessions without importing the lease package.
type Leases struct {
	*lease.Manager
	clk *clock.GatedClock
}

// NewLeases creates Leases whose lease directory lives under root, which
// must be a directory only the test uses.
func NewLeases(root string) (*Leases, error) {
	clk := clock.NewGatedClock(lease.InodeRecheckPeriod.Milliseconds())
	manager, err := lease.NewManager(lease.Environment{
		GOOS:          "linux",
		Getenv:        func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": root}[k] },
		UserCacheDir:  func() (string, error) { return root, nil },
		Getuid:        os.Getuid,
		IsLocalFS:     func(string) (bool, error) { return true, nil },
		FallbackRoot:  root,
		MaxSocketPath: 4096,
	}, clk)
	if err != nil {
		return nil, err
	}
	return &Leases{Manager: manager, clk: clk}, nil
}

// Held reports whether any holder has the lease of the device with the
// given platform and hardware id.
func (f *Leases) Held(ctx context.Context, platform, hardwareID string) (bool, error) {
	status, err := f.Probe(ctx, lease.KeyFor(platform, hardwareID))
	return status.Held, err
}

// FireRecheck lets every lease watcher run one inode check now.
func (f *Leases) FireRecheck() { f.clk.Fire() }

// LockPathFor reports the lock file of the device with the given
// platform and hardware id.
func (f *Leases) LockPathFor(platform, hardwareID string) string {
	return f.LockPath(lease.KeyFor(platform, hardwareID))
}
