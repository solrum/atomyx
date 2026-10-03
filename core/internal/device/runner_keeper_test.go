package device

import (
	"context"
	"errors"
	"os"
	"testing"
)

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
