package device

import (
	"os"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

const lenderPid = foreignOwnerPid + 10

var lenderStart = time.Date(2026, time.October, 3, 8, 30, 0, 0, time.UTC)

func (f *launchFixture) liveLender() RunnerOwner {
	f.inspector.Set(process.ProcessInfo{PID: lenderPid, PGID: lenderPid, Command: "atomyx-mcp", StartTime: lenderStart})
	return RunnerOwner{Pid: lenderPid, StartTime: lenderStart}
}

func (f *launchFixture) readRecord(t *testing.T, udid string) RunnerState {
	t.Helper()
	state, err := ReadRunnerState(f.statePath(udid))
	if err != nil {
		t.Fatalf("record of %s: %v", udid, err)
	}
	return state
}

func TestRunnerKeeper_StopAll_HandsAnAdoptedRunnerBackToALiveLender(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveRunner(t, "SIM-1")
	lender := fx.liveLender()
	a := fx.newAdopter()
	leases := &leaseFake{}
	leases.onTake = func(udid string) {
		if got := fx.readRecord(t, udid).OwnerPid; got != adoptingOwnerPid {
			t.Errorf("owner when the lease was taken = %d, want the adopter %d until then", got, adoptingOwnerPid)
		}
	}
	a.keeper.TakeLeaseWith(leases.attempt)
	agent := a.provision(t, "SIM-1", IosAgentKindSimulator, nil, ProvisionOptions{Lender: lender})
	if !agent.Adopted {
		t.Fatal("runner was not adopted")
	}
	if got := fx.readRecord(t, "SIM-1"); got.LenderPid != lenderPid || !got.LenderStartTime.Equal(lenderStart) {
		t.Fatalf("recorded lender = %d started %v, want %d started %v", got.LenderPid, got.LenderStartTime, lenderPid, lenderStart)
	}

	a.keeper.StopAll()

	if calls := a.signals.recorded(); len(calls) != 0 {
		t.Fatalf("group signals = %+v, want the runner left running for the lender", calls)
	}
	got := fx.readRecord(t, "SIM-1")
	if got.RunnerID != left.RunnerID || got.OwnerPid != lenderPid || !got.OwnerStartTime.Equal(lenderStart) {
		t.Fatalf("record = %+v, want the same runner owned by the lender", got)
	}
	if got.LenderPid != 0 || !got.LenderStartTime.IsZero() {
		t.Fatalf("lender after the hand back = %d started %v, want it cleared", got.LenderPid, got.LenderStartTime)
	}
	if len(leases.taken) != 1 {
		t.Fatalf("leases taken = %v, want the lease taken once before the hand back", leases.taken)
	}
}

func TestRunnerKeeper_StopAll_StopsAnAdoptedRunnerWhoseLenderIsGone(t *testing.T) {
	tests := []struct {
		name  string
		leave func(f *launchFixture)
	}{
		{"lender process is gone", func(f *launchFixture) { f.inspector.Remove(lenderPid) }},
		{"lender pid was reused", func(f *launchFixture) {
			f.inspector.Set(process.ProcessInfo{PID: lenderPid, PGID: lenderPid, Command: "other", StartTime: lenderStart.Add(time.Hour)})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newLaunchFixture(t)
			left := fx.leaveRunner(t, "SIM-1")
			lender := fx.liveLender()
			a := fx.newAdopter()
			a.provision(t, "SIM-1", IosAgentKindSimulator, nil, ProvisionOptions{Lender: lender})
			tc.leave(fx)

			a.keeper.StopAll()

			calls := a.signals.recorded()
			if len(calls) != 1 || calls[0].pgid != left.Pgid || calls[0].sig != syscall.SIGTERM {
				t.Fatalf("group signals = %+v, want one SIGTERM to the adopted group %d", calls, left.Pgid)
			}
			if _, err := os.Stat(fx.statePath("SIM-1")); !os.IsNotExist(err) {
				t.Fatalf("record after the stop: %v, want it removed", err)
			}
		})
	}
}

func TestRunnerKeeper_StopAll_KeepsTheLenderOnTheRecordWhileTheLeaseIsHeld(t *testing.T) {
	fx := newLaunchFixture(t)
	fx.leaveRunner(t, "SIM-1")
	lender := fx.liveLender()
	a := fx.newAdopter()
	a.keeper.TakeLeaseWith((&leaseFake{heldBy: map[string]bool{"SIM-1": true}}).attempt)
	a.provision(t, "SIM-1", IosAgentKindSimulator, nil, ProvisionOptions{Lender: lender})

	a.keeper.StopAll()

	got := fx.readRecord(t, "SIM-1")
	if got.OwnerPid != adoptingOwnerPid || got.LenderPid != lenderPid {
		t.Fatalf("record = %+v, want the owner and the lender unchanged while another holder has the lease", got)
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_AdoptionWithoutALenderRecordsNoneAndStopsAtExit(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveRunner(t, "SIM-1")
	stale := fx.liveLender()
	fx.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.LenderPid, s.LenderStartTime = stale.Pid, stale.StartTime })
	a := fx.newAdopter()
	a.provision(t, "SIM-1", IosAgentKindSimulator, nil, ProvisionOptions{})
	if got := fx.readRecord(t, "SIM-1"); got.LenderPid != 0 || !got.LenderStartTime.IsZero() {
		t.Fatalf("recorded lender = %d started %v, want a plain adoption to clear it", got.LenderPid, got.LenderStartTime)
	}

	a.keeper.StopAll()

	calls := a.signals.recorded()
	if len(calls) != 1 || calls[0].pgid != left.Pgid {
		t.Fatalf("group signals = %+v, want the runner stopped because it has no lender", calls)
	}
}
