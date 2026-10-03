package device

import (
	"context"
	"testing"
	"time"

	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

func (f *reapFixture) reapState() ReapOutcome {
	f.t.Helper()
	out, err := f.reaper.ReapState(context.Background(), f.state)
	if err != nil {
		f.t.Fatalf("ReapState() error = %v", err)
	}
	return out
}

func TestReapState_SignalsTheStateGroupAndLeavesTheRecordFileAlone(t *testing.T) {
	f := newReapFixture(t, IosAgentKindDevice)
	f.write()

	out := f.reapState()

	if !out.Signaled || out.PGID != reapTestPid {
		t.Fatalf("ReapState() = %+v, want the state's group signaled", out)
	}
	if got := f.signals.recorded(); len(got) != 1 || got[0].pgid != reapTestPid {
		t.Fatalf("signals = %+v, want one signal to the state's group", got)
	}
	got, err := ReadRunnerState(f.path)
	if err != nil || got.RunnerID != f.state.RunnerID {
		t.Fatalf("record = %+v (%v), want it untouched", got, err)
	}
}

func TestReapState_AStateWhosePidNowBelongsToAnotherProcessIsNotSignaled(t *testing.T) {
	f := newReapFixture(t, IosAgentKindDevice)
	f.inspector.Set(process.ProcessInfo{
		PID: reapTestPid, PGID: reapTestPid, UID: reapTestUID, Command: "xcodebuild", StartTime: reapTestStart.Add(time.Hour),
	})

	out := f.reapState()

	if out.Discarded == "" {
		t.Fatalf("ReapState() = %+v, want the state discarded", out)
	}
	f.assertNoSignal()
}

func TestReapState_ASimulatorRunnerAppOutsideTheGroupIsTerminatedThroughSimctl(t *testing.T) {
	f := newReapFixture(t, IosAgentKindSimulator)
	f.runner.Stub(process.ExactMatch("xcrun", "simctl", "terminate", reapTestUDID, DefaultRunnerBundleID), process.Result{}, nil)
	f.answering = func() (iosdriver.RunnerIdentity, bool) {
		if len(f.runner.Invocations()) > 0 {
			return iosdriver.RunnerIdentity{}, false
		}
		return iosdriver.RunnerIdentity{RunnerID: f.state.RunnerID, SimulatorUDID: reapTestUDID}, true
	}

	out := f.reapState()

	if !out.TerminatedViaSimctl {
		t.Fatalf("ReapState() = %+v, want the surviving runner app terminated", out)
	}
}

func TestReapState_ASimulatorPortAnsweredByAnotherLaunchIsLeftAlone(t *testing.T) {
	f := newReapFixture(t, IosAgentKindSimulator)
	f.inspector.Remove(reapTestPid)
	f.answering = func() (iosdriver.RunnerIdentity, bool) {
		return iosdriver.RunnerIdentity{RunnerID: "launched-later", SimulatorUDID: reapTestUDID}, true
	}

	out := f.reapState()

	if out.TerminatedViaSimctl || len(f.runner.Invocations()) != 0 {
		t.Fatalf("ReapState() = %+v, invocations = %+v, want a later launch left alone", out, f.runner.Invocations())
	}
}
