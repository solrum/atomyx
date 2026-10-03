package device

import (
	"context"
	"errors"
	"os"
	"testing"
)

// leaseFake scripts the keeper's lease attempt and records how it was
// used.
type leaseFake struct {
	heldBy   map[string]bool
	taken    []string
	released []string
	// onTake runs after a lease is taken and before the keeper reads the
	// record again.
	onTake func(udid string)
}

func (l *leaseFake) attempt(udid string) (func(), error) {
	if l.heldBy[udid] {
		return nil, errors.New("held")
	}
	l.taken = append(l.taken, udid)
	if l.onTake != nil {
		l.onTake(udid)
	}
	return func() { l.released = append(l.released, udid) }, nil
}

func TestRunnerKeeper_StopAll_StopsTheRunnersThisProcessOwns(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	if _, _, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator); err != nil {
		t.Fatalf("EnsureIOSAgent(SIM-1) error = %v", err)
	}
	if _, _, err := p.EnsureIOSAgent(context.Background(), "DEV-1", IosAgentKindDevice); err != nil {
		t.Fatalf("EnsureIOSAgent(DEV-1) error = %v", err)
	}

	fx.keeper.StopAll()

	for _, runner := range fx.farm.started("xcodebuild") {
		if !runner.Killed() {
			t.Error("StopAll left a runner running")
		}
	}
	if !fx.farm.started("iproxy")[0].Killed() {
		t.Error("StopAll left a tunnel running")
	}
	for _, udid := range []string{"SIM-1", "DEV-1"} {
		if _, err := os.Stat(fx.statePath(udid)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("record of %s after StopAll: %v, want it removed", udid, err)
		}
	}
}

func TestRunnerKeeper_StopAll_LeavesARunnerAnotherProcessOwns(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	if _, _, err := p.EnsureIOSAgent(context.Background(), "DEV-1", IosAgentKindDevice); err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	record, err := ReadRunnerState(fx.statePath("DEV-1"))
	if err != nil {
		t.Fatal(err)
	}
	record.OwnerPid = testOwnerPid + 1
	if err := WriteRunnerState(fx.statePath("DEV-1"), record); err != nil {
		t.Fatal(err)
	}

	fx.keeper.StopAll()

	if fx.farm.started("xcodebuild")[0].Killed() || fx.farm.started("iproxy")[0].Killed() {
		t.Error("StopAll stopped a runner whose record names another owner")
	}
	if kept, err := ReadRunnerState(fx.statePath("DEV-1")); err != nil || kept.OwnerPid != testOwnerPid+1 {
		t.Errorf("record = %+v (%v), want it kept with its owner", kept, err)
	}
}

func TestRunnerKeeper_ForgetsARunnerStoppedThroughItsKill(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	_, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	if err := kill(); err != nil {
		t.Fatalf("kill() error = %v", err)
	}
	fx.keeper.mu.Lock()
	kept := len(fx.keeper.runners)
	fx.keeper.mu.Unlock()
	if kept != 0 {
		t.Errorf("kept runners after kill = %d, want 0", kept)
	}
}

func TestRunnerKeeper_StopAll_RunsOnce(t *testing.T) {
	fx := newLaunchFixture(t)
	stops := 0
	fx.keeper.keep(launchRecord{runnerID: "r1"}, func() error { stops++; return nil })
	fx.keeper.StopAll()
	fx.keeper.StopAll()
	if stops != 1 {
		t.Errorf("stop ran %d times, want 1", stops)
	}
}

func TestRunnerKeeper_StopAll_SkipsARunnerWhoseDeviceLeaseIsHeld(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	if _, _, err := p.EnsureIOSAgent(context.Background(), "DEV-1", IosAgentKindDevice); err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	leases := &leaseFake{heldBy: map[string]bool{"DEV-1": true}}
	fx.keeper.TakeLeaseWith(leases.attempt)

	fx.keeper.StopAll()

	if fx.farm.started("xcodebuild")[0].Killed() || fx.farm.started("iproxy")[0].Killed() {
		t.Error("StopAll stopped a runner whose device lease another holder has")
	}
	if _, err := os.Stat(fx.statePath("DEV-1")); err != nil {
		t.Errorf("record after a skipped stop: %v, want it kept", err)
	}
}

func TestRunnerKeeper_StopAll_StopsTheRunnerOnceItTakesTheLeaseAndReleasesTheLeaseAfter(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	if _, _, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator); err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	leases := &leaseFake{}
	leases.onTake = func(string) {
		if fx.farm.started("xcodebuild")[0].Killed() {
			t.Error("the runner was stopped before the lease was taken")
		}
	}
	fx.keeper.TakeLeaseWith(leases.attempt)

	fx.keeper.StopAll()

	if !fx.farm.started("xcodebuild")[0].Killed() {
		t.Error("StopAll left the runner running although it took the lease and still owns it")
	}
	if len(leases.taken) != 1 || leases.taken[0] != "SIM-1" || len(leases.released) != 1 {
		t.Errorf("leases taken %v released %v, want SIM-1 taken once and released once", leases.taken, leases.released)
	}
}

func TestRunnerKeeper_StopAll_RereadsTheOwnerAfterTakingTheLease(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	if _, _, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator); err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	leases := &leaseFake{}
	leases.onTake = func(string) {
		fx.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.OwnerPid = testOwnerPid + 1 })
	}
	fx.keeper.TakeLeaseWith(leases.attempt)

	fx.keeper.StopAll()

	if fx.farm.started("xcodebuild")[0].Killed() {
		t.Error("StopAll stopped a runner that another process adopted while the keeper took the lease")
	}
	if len(leases.released) != 1 {
		t.Errorf("leases released = %v, want the lease released although the runner was skipped", leases.released)
	}
}
