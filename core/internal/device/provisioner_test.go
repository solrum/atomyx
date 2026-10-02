package device

import (
	"context"
	"testing"
)

// TestFakeProvisioner_EnsureIOSAgent_KillIsNonNilAndTracksInvocations
// guards the plumbing FakeProvisioner exists to exercise: a caller
// must always get back a callable kill, and invoking it must be
// observable, so a test built on FakeProvisioner can assert a
// teardown path actually stopped the simulated driver process instead
// of leaking it.
func TestFakeProvisioner_EnsureIOSAgent_KillIsNonNilAndTracksInvocations(t *testing.T) {
	p := NewFakeProvisioner()

	_, kill, err := p.EnsureIOSAgent(context.Background(), "udid-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	if kill == nil {
		t.Fatal("EnsureIOSAgent() kill = nil, want a non-nil kill func")
	}
	if got := p.IOSKillCount("udid-1"); got != 0 {
		t.Fatalf("IOSKillCount() = %d before kill is called, want 0", got)
	}

	if err := kill(); err != nil {
		t.Fatalf("kill() error = %v", err)
	}
	if got := p.IOSKillCount("udid-1"); got != 1 {
		t.Fatalf("IOSKillCount() = %d after kill is called once, want 1", got)
	}
}

// TestFakeProvisioner_EnsureIOSAgent_CanceledGate_ReturnsNonNilKill
// guards against an asymmetric mock: DriverProvisioner's real
// EnsureIOSAgent never returns a nil kill, even on failure, so
// FakeProvisioner must not either, or a consumer written against the
// fake would panic against the real implementation's contract holding
// but not the fake's.
func TestFakeProvisioner_EnsureIOSAgent_CanceledGate_ReturnsNonNilKill(t *testing.T) {
	p := NewFakeProvisioner()
	p.SetGate(make(chan struct{})) // never closed

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, kill, err := p.EnsureIOSAgent(ctx, "udid-1", IosAgentKindSimulator)
	if err == nil {
		t.Fatal("EnsureIOSAgent() error = nil, want the canceled ctx error")
	}
	if kill == nil {
		t.Fatal("EnsureIOSAgent() kill = nil, want a non-nil no-op kill even on failure")
	}
	if err := kill(); err != nil {
		t.Fatalf("kill() error = %v, want a no-op success", err)
	}
}
