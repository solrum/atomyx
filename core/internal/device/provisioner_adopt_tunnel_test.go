package device

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

// leaveDeviceRunner launches a physical device runner and its tunnel
// through a provisioner whose process then ends without stopping them, and
// returns the record. The launching process is gone from the inspector
// afterwards, and the tunnel keeps listening.
func (f *launchFixture) leaveDeviceRunner(t *testing.T, udid string) RunnerState {
	t.Helper()
	first := f.provisioner(nil)
	if _, _, err := first.EnsureIOSAgent(context.Background(), udid, IosAgentKindDevice); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	state, err := ReadRunnerState(f.statePath(udid))
	if err != nil {
		t.Fatal(err)
	}
	f.inspector.Remove(testOwnerPid)
	return state
}

// endTunnel makes the recorded tunnel go away: its process disappears and
// its port stops listening.
func (f *launchFixture) endTunnel(left RunnerState) {
	f.inspector.Remove(left.TunnelPid)
	_ = f.farm.started("iproxy")[0].Kill()
}

func (a *adopter) signaledGroups() map[int][]syscall.Signal {
	out := map[int][]syscall.Signal{}
	for _, call := range a.signals.recorded() {
		out[call.pgid] = append(out[call.pgid], call.sig)
	}
	return out
}

func TestDriverProvisioner_ProvisionIOSAgent_PhysicalDevice_AdoptsTheRunnerAndItsRecordedTunnel(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveDeviceRunner(t, "DEV-1")
	a := fx.newAdopter()

	agent := a.provision(t, "DEV-1", IosAgentKindDevice, nil, ProvisionOptions{})

	if !agent.Adopted || agent.Port != left.HostPort || agent.RunnerID != left.RunnerID {
		t.Fatalf("agent = %+v, want the left runner on port %d with id %q adopted", agent, left.HostPort, left.RunnerID)
	}
	if runners, tunnels := len(fx.farm.started("xcodebuild")), len(fx.farm.started("iproxy")); runners != 1 || tunnels != 1 {
		t.Fatalf("started runners = %d tunnels = %d, want the left ones and nothing new", runners, tunnels)
	}
	state, err := ReadRunnerState(fx.statePath("DEV-1"))
	if err != nil {
		t.Fatal(err)
	}
	if state.TunnelPid != left.TunnelPid || !state.TunnelStartTime.Equal(left.TunnelStartTime) {
		t.Fatalf("record tunnel = %d started %v, want the adopted tunnel %d recorded", state.TunnelPid, state.TunnelStartTime, left.TunnelPid)
	}
	if state.OwnerPid != adoptingOwnerPid {
		t.Fatalf("owner = %d, want the adopting process %d", state.OwnerPid, adoptingOwnerPid)
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_PhysicalDevice_StopsAnAdoptedTunnelThroughTheGuardedPath(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveDeviceRunner(t, "DEV-1")
	a := fx.newAdopter()
	a.provision(t, "DEV-1", IosAgentKindDevice, nil, ProvisionOptions{})

	a.keeper.StopAll()

	groups := a.signaledGroups()
	if got := groups[left.TunnelPid]; len(got) != 1 || got[0] != syscall.SIGTERM {
		t.Fatalf("tunnel signals = %v, want one SIGTERM through the reaper", got)
	}
	if got := groups[left.Pgid]; len(got) != 1 || got[0] != syscall.SIGTERM {
		t.Fatalf("runner signals = %v, want one SIGTERM", got)
	}
	if _, err := os.Stat(fx.statePath("DEV-1")); !os.IsNotExist(err) {
		t.Fatalf("record after the keeper stopped: %v, want it removed", err)
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_PhysicalDevice_StartsANewTunnelForARecordedTunnelThatIsGoneOrNotTheRecordedProcess(t *testing.T) {
	tests := []struct {
		name string
		end  func(f *launchFixture, left RunnerState)
	}{
		{"the tunnel process is gone", func(f *launchFixture, left RunnerState) { f.endTunnel(left) }},
		{"the tunnel pid was reused by a later process", func(f *launchFixture, left RunnerState) {
			f.endTunnel(left)
			f.inspector.Set(process.ProcessInfo{PID: left.TunnelPid, PGID: left.TunnelPid, UID: testRunnerUID, Command: "iproxy", StartTime: testProcessStart.Add(time.Hour)})
		}},
		{"the tunnel pid is another command", func(f *launchFixture, left RunnerState) {
			f.endTunnel(left)
			f.inspector.Set(process.ProcessInfo{PID: left.TunnelPid, PGID: left.TunnelPid, UID: testRunnerUID, Command: "sleep", StartTime: testProcessStart})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newLaunchFixture(t)
			left := fx.leaveDeviceRunner(t, "DEV-1")
			tc.end(fx, left)
			a := fx.newAdopter()

			agent := a.provision(t, "DEV-1", IosAgentKindDevice, nil, ProvisionOptions{})

			if !agent.Adopted {
				t.Fatalf("agent = %+v, want the runner adopted behind a new tunnel", agent)
			}
			tunnels := fx.farm.started("iproxy")
			if len(tunnels) != 2 || len(fx.farm.started("xcodebuild")) != 1 {
				t.Fatalf("started tunnels = %d runners = %d, want one new tunnel and no new runner", len(tunnels), len(fx.farm.started("xcodebuild")))
			}
			invocation := fx.farm.Invocations()[len(fx.farm.Invocations())-1]
			if want := left.HostPort; !strings.HasPrefix(invocation.Args[0], strconv.Itoa(want)+":") {
				t.Fatalf("new tunnel args = %v, want the recorded port %d", invocation.Args, want)
			}
			state, err := ReadRunnerState(fx.statePath("DEV-1"))
			if err != nil {
				t.Fatal(err)
			}
			if newPid := tunnels[1].Pid(); state.TunnelPid != newPid || state.TunnelPid == left.TunnelPid || !state.TunnelStartTime.Equal(testProcessStart) {
				t.Fatalf("record tunnel = %d started %v, want the new tunnel %d recorded", state.TunnelPid, state.TunnelStartTime, newPid)
			}
			if got := a.signaledGroups()[left.TunnelPid]; len(got) != 0 {
				t.Fatalf("signals to the stale tunnel pid = %v, want none", got)
			}

			a.keeper.StopAll()
			if !tunnels[1].Killed() {
				t.Error("the new tunnel was left running when the keeper stopped the runner")
			}
		})
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_PhysicalDevice_DoesNotAdoptBehindAListenerItCannotAccountFor(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveDeviceRunner(t, "DEV-1")
	fx.inspector.Set(process.ProcessInfo{PID: left.TunnelPid, PGID: left.TunnelPid, UID: testRunnerUID, Command: "sleep", StartTime: testProcessStart})
	a := fx.newAdopter()

	agent := a.provision(t, "DEV-1", IosAgentKindDevice, nil, ProvisionOptions{})
	defer func() { _ = agent.Kill() }()

	if agent.Adopted || agent.RunnerID == left.RunnerID {
		t.Fatalf("agent = %+v, want a newly launched runner", agent)
	}
	reasons := a.reasons.recorded()
	if len(reasons) != 1 || !strings.Contains(reasons[0], "not the recorded one") {
		t.Fatalf("reasons = %v, want one saying the tunnel is not the recorded one", reasons)
	}
	if got := a.signaledGroups()[left.TunnelPid]; len(got) != 0 {
		t.Fatalf("signals to the unrelated process = %v, want none", got)
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_PhysicalDevice_AdoptedTunnelThatEndsIsReportedLost(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveDeviceRunner(t, "DEV-1")
	a := fx.newAdopter()
	ticks := newWatchTicks()
	a.p.awaitWatchTick = ticks.await
	lost := make(chan error, 1)
	a.provision(t, "DEV-1", IosAgentKindDevice, func(cause error) { lost <- cause }, ProvisionOptions{})

	ticks.check()
	select {
	case cause := <-lost:
		t.Fatalf("a live tunnel was reported lost: %v", cause)
	default:
	}
	fx.inspector.Remove(left.TunnelPid)
	ticks.tick <- struct{}{}
	select {
	case <-ticks.waiting:
		t.Fatal("the watcher finished a check without reporting that the tunnel ended")
	case cause := <-lost:
		lost <- cause
	}

	if exit := awaitLoss(t, lost); exit.Component != LostTunnel {
		t.Fatalf("component = %q, want %q", exit.Component, LostTunnel)
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_PhysicalDevice_StopsTheTunnelItStartedWhenTheAdoptionFails(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveDeviceRunner(t, "DEV-1")
	fx.endTunnel(left)
	fx.rewriteRecord(t, "DEV-1", func(s *RunnerState) { s.RunnerID = "someone-else" })
	a := fx.newAdopter()
	lost := make(chan error, 1)

	agent := a.provision(t, "DEV-1", IosAgentKindDevice, func(cause error) { lost <- cause }, ProvisionOptions{})
	defer func() { _ = agent.Kill() }()

	if agent.Adopted {
		t.Fatalf("agent = %+v, want a newly launched runner", agent)
	}
	if abandoned := fx.farm.started("iproxy")[1]; !abandoned.Killed() {
		t.Error("the tunnel started for the failed adoption was left running")
	}
	select {
	case cause := <-lost:
		t.Fatalf("the abandoned tunnel's exit was reported as a loss: %v", cause)
	default:
	}
}
