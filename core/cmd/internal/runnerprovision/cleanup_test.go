package runnerprovision_test

import (
	"context"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/cmd/internal/runnerprovision"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/shutdown"
)

const cleanupTestTimeout = 10 * time.Second

// keptRunner launches one runner through a provisioner that hands it to
// a fresh keeper, and returns the keeper and the provisioner.
func keptRunner(t *testing.T) (*device.RunnerKeeper, *device.FakeProvisioner) {
	t.Helper()
	keeper := device.NewRunnerKeeper()
	prov := device.NewFakeProvisioner()
	prov.KeepWith(keeper)
	if _, _, err := prov.EnsureIOSAgent(context.Background(), "SIM-1", device.IosAgentKindSimulator); err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	return keeper, prov
}

func TestCleanup_NormalExitStopsTheRunnerTheProcessOwns(t *testing.T) {
	keeper, prov := keptRunner(t)

	finished := shutdown.Bounded(clock.SystemClock{}, shutdown.GracePeriod, runnerprovision.Cleanup(keeper, nil))

	if !finished {
		t.Fatal("the exit cleanup did not finish within the grace period")
	}
	if got := prov.IOSKillCount("SIM-1"); got != 1 {
		t.Fatalf("runner stop count = %d, want 1 so a command that exits leaves no runner", got)
	}
}

func TestCleanup_InterruptStopsTheRunnerTheProcessOwns(t *testing.T) {
	keeper, prov := keptRunner(t)
	clk := clock.NewManualClock(0)
	exited := make(chan int, 1)

	shutdown.HandleStop(clk, syscall.SIGINT, shutdown.StopHooks{
		Cancel:  func() {},
		Cleanup: runnerprovision.Cleanup(keeper, nil),
		Exit:    func(code int) { exited <- code },
	})
	clk.AwaitSleepers(1)
	clk.Advance((shutdown.GracePeriod - shutdown.ForcedCleanupBudget).Milliseconds())

	select {
	case code := <-exited:
		if code != shutdown.ExitCodeSigint {
			t.Fatalf("exit code = %d, want %d", code, shutdown.ExitCodeSigint)
		}
	case <-time.After(cleanupTestTimeout):
		t.Fatal("the process never exited")
	}
	if got := prov.IOSKillCount("SIM-1"); got != 1 {
		t.Fatalf("runner stop count after the interrupt = %d, want 1", got)
	}
}

func TestCleanup_StopsTheRunnersAfterTheBaseCleanup(t *testing.T) {
	keeper, prov := keptRunner(t)
	var stoppedBeforeBase int

	runnerprovision.Cleanup(keeper, func() { stoppedBeforeBase = prov.IOSKillCount("SIM-1") })()

	if stoppedBeforeBase != 0 {
		t.Fatalf("runner stop count seen by the base cleanup = %d, want 0 so the base releases the session lease first", stoppedBeforeBase)
	}
	if got := prov.IOSKillCount("SIM-1"); got != 1 {
		t.Fatalf("runner stop count after the cleanup = %d, want 1", got)
	}
}
