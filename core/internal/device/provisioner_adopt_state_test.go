package device

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

const (
	laterRunnerPid = 7300
	laterRunnerID  = "launched-by-another-process"
)

// replaceRecordWithLaterLaunch models another process that launched its
// own live runner for udid after the adopter did, so the record on disk
// names a different runner, process and owner.
func (f *launchFixture) replaceRecordWithLaterLaunch(t *testing.T, udid string, left RunnerState) {
	t.Helper()
	f.inspector.Set(process.ProcessInfo{PID: laterRunnerPid, PGID: laterRunnerPid, UID: testRunnerUID, Command: "xcodebuild", StartTime: testProcessStart})
	later := left
	later.RunnerID, later.HostPort = laterRunnerID, left.HostPort+1
	later.Pid, later.Pgid = laterRunnerPid, laterRunnerPid
	later.OwnerPid, later.OwnerStartTime = foreignOwnerPid, testOwnerStart
	if err := WriteRunnerState(f.statePath(udid), later); err != nil {
		t.Fatal(err)
	}
}

func (f *launchFixture) assertLaterLaunchUntouched(t *testing.T, udid string, signals *recordingSignaler) {
	t.Helper()
	for _, call := range signals.recorded() {
		if call.pgid == laterRunnerPid {
			t.Fatalf("group signals = %+v, want the later launch never signaled", signals.recorded())
		}
	}
	state, err := ReadRunnerState(f.statePath(udid))
	if err != nil || state.RunnerID != laterRunnerID {
		t.Fatalf("record = %+v (%v), want the later launch's record kept", state, err)
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_AdoptedRunnerWatcherNeverTouchesALaterLaunch(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveRunner(t, "SIM-1")
	a := fx.newAdopter()
	ticks := newWatchTicks()
	a.p.awaitWatchTick = ticks.await
	lost := make(chan error, 1)
	agent := a.provision(t, "SIM-1", IosAgentKindSimulator, func(cause error) { lost <- cause }, ProvisionOptions{})
	if !agent.Adopted {
		t.Fatal("runner was not adopted")
	}
	fx.replaceRecordWithLaterLaunch(t, "SIM-1", left)
	fx.inspector.Remove(left.Pid)

	<-ticks.waiting
	ticks.tick <- struct{}{}

	_ = awaitLoss(t, lost)
	fx.assertLaterLaunchUntouched(t, "SIM-1", a.signals)
}

func TestDriverProvisioner_ProvisionIOSAgent_KeeperStopOfAnAdoptedRunnerNeverTouchesALaterLaunch(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveRunner(t, "SIM-1")
	a := fx.newAdopter()
	agent := a.provision(t, "SIM-1", IosAgentKindSimulator, nil, ProvisionOptions{})
	if !agent.Adopted {
		t.Fatal("runner was not adopted")
	}
	fx.replaceRecordWithLaterLaunch(t, "SIM-1", left)

	a.keeper.StopAll()

	calls := a.signals.recorded()
	if len(calls) != 1 || calls[0].pgid != left.Pgid || calls[0].sig != syscall.SIGTERM {
		t.Fatalf("group signals = %+v, want one SIGTERM to the adopted group %d only", calls, left.Pgid)
	}
	fx.assertLaterLaunchUntouched(t, "SIM-1", a.signals)
}

func TestDriverProvisioner_EnsureIOSAgent_StopEndsTheSimulatorRunnerAppThatOutlivesItsGroup(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	signals := &recordingSignaler{inspector: fx.inspector, exitOn: map[syscall.Signal]bool{syscall.SIGTERM: true}}
	var runnerID string
	fx.runner.Stub(process.ExactMatch("xcrun", "simctl", "terminate", "SIM-1", iosdriver.DefaultRunnerBundleID), process.Result{}, nil)
	p.deps.Reaper = NewRunnerReaper(RunnerReaperDeps{
		Inspector: fx.inspector, Runner: fx.runner, Clock: clock.NewFakeClock(0), Signals: signals,
		Prober: func(context.Context, int) (iosdriver.RunnerIdentity, bool) {
			if len(fx.runner.Invocations()) > 0 {
				return iosdriver.RunnerIdentity{}, false
			}
			return iosdriver.RunnerIdentity{RunnerID: runnerID, SimulatorUDID: "SIM-1"}, true
		},
		OwnPGID: orphanOwnPGID, OwnUID: orphanUID, GracePeriod: time.Second,
	})
	_, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	state, err := ReadRunnerState(fx.statePath("SIM-1"))
	if err != nil {
		t.Fatal(err)
	}
	runnerID = state.RunnerID

	if err := kill(); err != nil {
		t.Fatalf("kill() error = %v", err)
	}

	terminated := 0
	for _, inv := range fx.runner.Invocations() {
		if len(inv.Args) > 1 && inv.Args[1] == "terminate" {
			terminated++
		}
	}
	if terminated != 1 {
		t.Fatalf("simctl terminate calls = %d, want the surviving runner app ended once", terminated)
	}
	if _, err := os.Stat(fx.statePath("SIM-1")); !os.IsNotExist(err) {
		t.Fatalf("record after kill: %v, want it removed", err)
	}
}
