package runnerprovision

import (
	"context"
	"os"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
)

const sessionLeaseTestUDID = "SIM-1"

func newLeaseManager(t *testing.T) *lease.Manager {
	t.Helper()
	root := t.TempDir()
	manager, err := lease.NewManager(lease.Environment{
		GOOS:          "darwin",
		Getenv:        func(string) string { return "" },
		UserCacheDir:  func() (string, error) { return root, nil },
		Getuid:        os.Getuid,
		IsLocalFS:     func(string) (bool, error) { return true, nil },
		FallbackRoot:  root,
		MaxSocketPath: 4096,
	}, clock.SystemClock{})
	if err != nil {
		t.Fatalf("lease.NewManager() error = %v", err)
	}
	return manager
}

func TestCleanup_KeeperStopsTheRunnerOnceTheBaseReleasesTheSessionLease(t *testing.T) {
	manager := newLeaseManager(t)
	keeper := device.NewRunnerKeeper().TakeLeaseWith(leaseAttempt(manager, os.Getpid(), cleanupHolderCommand))
	prov := device.NewFakeProvisioner()
	prov.KeepWith(keeper)
	if _, _, err := prov.EnsureIOSAgent(context.Background(), sessionLeaseTestUDID, device.IosAgentKindSimulator); err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	session, err := manager.Acquire(context.Background(), lease.KeyFor(string(device.PlatformIOS), sessionLeaseTestUDID), lease.Holder{
		PID: os.Getpid(), Command: "atomyx-mcp", Role: "session", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	Cleanup(keeper, func() { _ = session.Release() })()

	if got := prov.IOSKillCount(sessionLeaseTestUDID); got != 1 {
		t.Fatalf("runner stop count = %d, want 1 because the base released the session lease before the keeper took it", got)
	}
}

func TestCleanup_KeeperLeavesTheRunnerWhileTheSessionLeaseIsStillHeld(t *testing.T) {
	manager := newLeaseManager(t)
	keeper := device.NewRunnerKeeper().TakeLeaseWith(leaseAttempt(manager, os.Getpid(), cleanupHolderCommand))
	prov := device.NewFakeProvisioner()
	prov.KeepWith(keeper)
	if _, _, err := prov.EnsureIOSAgent(context.Background(), sessionLeaseTestUDID, device.IosAgentKindSimulator); err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	session, err := manager.Acquire(context.Background(), lease.KeyFor(string(device.PlatformIOS), sessionLeaseTestUDID), lease.Holder{
		PID: os.Getpid(), Command: "atomyx-mcp", Role: "session", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer func() { _ = session.Release() }()

	Cleanup(keeper, nil)()

	if got := prov.IOSKillCount(sessionLeaseTestUDID); got != 0 {
		t.Fatalf("runner stop count = %d, want 0 because a session still holds the device lease", got)
	}
}
