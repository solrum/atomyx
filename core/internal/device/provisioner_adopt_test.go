package device

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
)

const (
	adoptingOwnerPid = testOwnerPid + 2
	foreignOwnerPid  = testOwnerPid + 1
)

var adoptingOwnerStart = time.Date(2026, time.October, 3, 9, 0, 0, 0, time.UTC)

// reasonLogger records the reason of every "not adopting" line, passes
// Debug messages on a channel, and drops the rest.
type reasonLogger struct {
	mu      sync.Mutex
	reasons []string
	// debug receives every Debug message.
	debug chan string
}

func (l *reasonLogger) Debug(msg string, _ logging.LogContext) {
	select {
	case l.debug <- msg:
	default:
	}
}
func (l *reasonLogger) Info(msg string, fields logging.LogContext) {
	if msg != "not adopting the recorded runner" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reasons = append(l.reasons, fields["reason"].(string))
}
func (l *reasonLogger) Warn(string, logging.LogContext)         {}
func (l *reasonLogger) Error(string, error, logging.LogContext) {}
func (l *reasonLogger) Child(logging.LogContext) logging.Logger { return l }

func (l *reasonLogger) recorded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.reasons...)
}

// leaveRunner launches a simulator runner through a provisioner whose
// process then ends without stopping it, the way a killed holder leaves
// it, and returns its record. The launching process is gone from the
// inspector afterwards.
func (f *launchFixture) leaveRunner(t *testing.T, udid string) RunnerState {
	t.Helper()
	first := f.provisioner(nil)
	if _, _, err := first.EnsureIOSAgent(context.Background(), udid, IosAgentKindSimulator); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	state, err := ReadRunnerState(f.statePath(udid))
	if err != nil {
		t.Fatal(err)
	}
	f.inspector.Remove(testOwnerPid)
	return state
}

// watchTicks drives the watcher of an adopted runner one check at a time.
// await announces that the previous check has finished, then waits for a
// tick.
type watchTicks struct{ waiting, tick chan struct{} }

func newWatchTicks() watchTicks {
	return watchTicks{waiting: make(chan struct{}), tick: make(chan struct{})}
}

func (w watchTicks) await(ctx context.Context) error {
	select {
	case w.waiting <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-w.tick:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// check runs one watcher check and returns once it has finished.
func (w watchTicks) check() {
	<-w.waiting
	w.tick <- struct{}{}
	<-w.waiting
}

type adopter struct {
	p       *DriverProvisioner
	keeper  *RunnerKeeper
	signals *recordingSignaler
	reasons *reasonLogger
}

// newAdopter builds the provisioner of a later process that runs as
// adoptingOwnerPid. Its watcher waits for ctx unless a test installs its
// own ticks.
func (f *launchFixture) newAdopter() *adopter {
	f.inspector.Set(process.ProcessInfo{PID: adoptingOwnerPid, PGID: adoptingOwnerPid, Command: "atomyx", StartTime: adoptingOwnerStart})
	signals := &recordingSignaler{inspector: f.inspector, exitOn: map[syscall.Signal]bool{syscall.SIGTERM: true}}
	reasons := &reasonLogger{debug: make(chan string, 4)}
	p := f.reapingProvisioner(reasons, signals)
	p.ownPid = func() int { return adoptingOwnerPid }
	p.awaitWatchTick = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	keeper := NewRunnerKeeper()
	p.deps.Keeper = keeper
	return &adopter{p: p, keeper: keeper, signals: signals, reasons: reasons}
}

func (a *adopter) provision(t *testing.T, udid string, kind IosAgentKind, onLost func(error), opts ProvisionOptions) IOSAgent {
	t.Helper()
	agent, err := a.p.ProvisionIOSAgent(context.Background(), udid, kind, onLost, opts)
	if err != nil {
		t.Fatalf("ProvisionIOSAgent() error = %v", err)
	}
	return agent
}

func (f *launchFixture) runnerApp() string {
	return filepath.Join(f.projectDir, "build", "Build", "Products", "Debug-iphonesimulator", "AtomyxDriverAgent-Runner.app")
}

func (f *launchFixture) rewriteRecord(t *testing.T, udid string, edit func(*RunnerState)) {
	t.Helper()
	state, err := ReadRunnerState(f.statePath(udid))
	if err != nil {
		t.Fatal(err)
	}
	edit(&state)
	if err := WriteRunnerState(f.statePath(udid), state); err != nil {
		t.Fatal(err)
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_AdoptsARunnerWhoseEveryConditionHolds(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *launchFixture)
	}{
		{"owner is another live process", func(f *launchFixture) {
			f.inspector.Set(process.ProcessInfo{PID: foreignOwnerPid, PGID: foreignOwnerPid, Command: "atomyx", StartTime: testOwnerStart})
			f.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.OwnerPid, s.OwnerStartTime = foreignOwnerPid, testOwnerStart })
		}},
		{"owner is this process", func(f *launchFixture) {
			f.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.OwnerPid, s.OwnerStartTime = adoptingOwnerPid, adoptingOwnerStart })
		}},
		{"owner process is gone", func(*launchFixture) {}},
		{"owner pid belongs to a process that started later", func(f *launchFixture) {
			f.inspector.Set(process.ProcessInfo{PID: testOwnerPid, PGID: testOwnerPid, Command: "other", StartTime: testOwnerStart.Add(time.Hour)})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newLaunchFixture(t)
			left := fx.leaveRunner(t, "SIM-1")
			tc.setup(fx)
			a := fx.newAdopter()

			agent := a.provision(t, "SIM-1", IosAgentKindSimulator, nil, ProvisionOptions{})

			if !agent.Adopted || agent.Port != left.HostPort || agent.RunnerID != left.RunnerID {
				t.Fatalf("agent = %+v, want the left runner on port %d with id %q adopted", agent, left.HostPort, left.RunnerID)
			}
			if got := len(fx.farm.started("xcodebuild")); got != 1 {
				t.Fatalf("launched runners = %d, want none beyond the left one", got)
			}
			if calls := a.signals.recorded(); len(calls) != 0 {
				t.Fatalf("group signals = %+v, want none when adopting", calls)
			}
			state, err := ReadRunnerState(fx.statePath("SIM-1"))
			if err != nil {
				t.Fatal(err)
			}
			if state.OwnerPid != adoptingOwnerPid || !state.OwnerStartTime.Equal(adoptingOwnerStart) {
				t.Fatalf("owner = %d started %v, want the adopting process %d started %v", state.OwnerPid, state.OwnerStartTime, adoptingOwnerPid, adoptingOwnerStart)
			}

			a.keeper.StopAll()
			calls := a.signals.recorded()
			if len(calls) != 1 || calls[0].pgid != left.Pgid || calls[0].sig != syscall.SIGTERM {
				t.Fatalf("group signals after the keeper stopped = %+v, want one SIGTERM to the adopted group %d", calls, left.Pgid)
			}
			if _, err := os.Stat(fx.statePath("SIM-1")); !os.IsNotExist(err) {
				t.Fatalf("record after the keeper stopped: %v, want it removed", err)
			}
		})
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_EndsAndReplacesARunnerThatFailsAnAdoptionCondition(t *testing.T) {
	tests := []struct {
		name       string
		reason     string
		invalidate func(t *testing.T, f *launchFixture, left RunnerState)
	}{
		{"record names another device", "belongs to another device", func(t *testing.T, f *launchFixture, _ RunnerState) {
			f.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.UDID = "SIM-2" })
		}},
		{"record names a runner of another kind", "not a simulator runner", func(t *testing.T, f *launchFixture, _ RunnerState) {
			f.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.Kind = string(IosAgentKindDevice) })
		}},
		{"host rebooted since the record", "rebooted", func(t *testing.T, f *launchFixture, _ RunnerState) {
			f.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.HostBootTime = s.HostBootTime.Add(-time.Hour) })
		}},
		{"runner process is gone", "has gone", func(_ *testing.T, f *launchFixture, left RunnerState) { f.inspector.Remove(left.Pid) }},
		{"runner pid was reused", "different process", func(_ *testing.T, f *launchFixture, left RunnerState) {
			f.inspector.Set(process.ProcessInfo{PID: left.Pid, PGID: left.Pid, UID: testRunnerUID, Command: "xcodebuild", StartTime: testProcessStart.Add(time.Hour)})
		}},
		{"record carries no launch id", "carries no launch id", func(t *testing.T, f *launchFixture, _ RunnerState) {
			f.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.RunnerID = "" })
		}},
		{"record carries no port", "carries no launch id", func(t *testing.T, f *launchFixture, _ RunnerState) {
			f.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.HostPort = 0 })
		}},
		{"runner answers with another launch id", "does not answer", func(t *testing.T, f *launchFixture, _ RunnerState) {
			f.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.RunnerID = "someone-else" })
		}},
		{"nothing answers on the recorded port", "does not answer", func(_ *testing.T, f *launchFixture, _ RunnerState) {
			_ = f.farm.started("xcodebuild")[0].listener.Close()
		}},
		{"product was rebuilt", "different build", func(t *testing.T, f *launchFixture, _ RunnerState) {
			rebuilt := time.Now().Add(time.Hour)
			if err := os.Chtimes(f.runnerApp(), rebuilt, rebuilt); err != nil {
				t.Fatal(err)
			}
		}},
		{"product is older than a source", "different build", func(t *testing.T, f *launchFixture, _ RunnerState) {
			newer := time.Now().Add(2 * time.Hour)
			if err := os.Chtimes(filepath.Join(f.projectDir, "AtomyxDriver.xcodeproj", "project.pbxproj"), newer, newer); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newLaunchFixture(t)
			left := fx.leaveRunner(t, "SIM-1")
			tc.invalidate(t, fx, left)
			a := fx.newAdopter()

			agent := a.provision(t, "SIM-1", IosAgentKindSimulator, nil, ProvisionOptions{})
			defer func() { _ = agent.Kill() }()

			if agent.Adopted || agent.RunnerID == left.RunnerID {
				t.Fatalf("agent = %+v, want a newly launched runner", agent)
			}
			if got := len(fx.farm.started("xcodebuild")); got != 2 {
				t.Fatalf("launched runners = %d, want the left one and a replacement", got)
			}
			reasons := a.reasons.recorded()
			if len(reasons) != 1 || !strings.Contains(reasons[0], tc.reason) {
				t.Fatalf("reasons = %v, want one mentioning %q", reasons, tc.reason)
			}
		})
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_FreshEndsTheRecordedRunnerEvenWhenAdoptable(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveRunner(t, "SIM-1")
	a := fx.newAdopter()

	agent := a.provision(t, "SIM-1", IosAgentKindSimulator, nil, ProvisionOptions{Fresh: true})
	defer func() { _ = agent.Kill() }()

	if agent.Adopted {
		t.Fatal("agent adopted the recorded runner although a fresh one was requested")
	}
	calls := a.signals.recorded()
	if len(calls) != 1 || calls[0].pgid != left.Pgid {
		t.Fatalf("group signals = %+v, want the recorded runner's group ended", calls)
	}
	if got := len(fx.farm.started("xcodebuild")); got != 2 {
		t.Fatalf("launched runners = %d, want a replacement", got)
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_AdoptedRunnerWhoseProcessEndsIsReportedLost(t *testing.T) {
	tests := []struct {
		name string
		end  func(f *launchFixture, left RunnerState)
	}{
		{"process gone", func(f *launchFixture, left RunnerState) { f.inspector.Remove(left.Pid) }},
		{"pid reused", func(f *launchFixture, left RunnerState) {
			f.inspector.Set(process.ProcessInfo{PID: left.Pid, PGID: left.Pid, UID: testRunnerUID, Command: "xcodebuild", StartTime: testProcessStart.Add(time.Hour)})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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

			ticks.check()
			select {
			case cause := <-lost:
				t.Fatalf("a live runner was reported lost: %v", cause)
			default:
			}

			tc.end(fx, left)
			ticks.tick <- struct{}{}
			select {
			case <-ticks.waiting:
				t.Fatal("the watcher finished a check without reporting that the runner ended")
			case msg := <-a.reasons.debug:
				t.Fatalf("the loss was dropped: %s", msg)
			case cause := <-lost:
				lost <- cause
			}

			exit := awaitLoss(t, lost)
			if exit.Component != LostRunner {
				t.Errorf("component = %q, want %q", exit.Component, LostRunner)
			}
			if _, err := os.Stat(fx.statePath("SIM-1")); !os.IsNotExist(err) {
				t.Errorf("record after the loss: %v, want it removed by the provisioner's own stop", err)
			}
		})
	}
}

func TestDriverProvisioner_ProvisionIOSAgent_AdoptedRunnerTakenOverByAnotherProcessIsNeitherStoppedNorReportedLost(t *testing.T) {
	fx := newLaunchFixture(t)
	left := fx.leaveRunner(t, "SIM-1")
	logger := debugSignalLogger{debug: make(chan string, 1)}
	a := fx.newAdopter()
	a.p.logger = logger
	ticks := newWatchTicks()
	a.p.awaitWatchTick = ticks.await
	lost := make(chan error, 1)
	agent := a.provision(t, "SIM-1", IosAgentKindSimulator, func(cause error) { lost <- cause }, ProvisionOptions{})
	if !agent.Adopted {
		t.Fatal("runner was not adopted")
	}
	fx.rewriteRecord(t, "SIM-1", func(s *RunnerState) { s.OwnerPid = foreignOwnerPid })
	fx.inspector.Remove(left.Pid)

	<-ticks.waiting
	ticks.tick <- struct{}{}

	select {
	case <-logger.debug:
	case <-ticks.waiting:
		t.Fatal("the watcher finished a check without noticing that another process owns the runner")
	case cause := <-lost:
		t.Fatalf("a runner owned by another process was reported lost: %v", cause)
	}
	if err := agent.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if calls := a.signals.recorded(); len(calls) != 0 {
		t.Fatalf("group signals = %+v, want none for a runner another process owns", calls)
	}
	kept, err := ReadRunnerState(fx.statePath("SIM-1"))
	if err != nil || kept.OwnerPid != foreignOwnerPid {
		t.Fatalf("record = %+v (%v), want it kept with its new owner", kept, err)
	}
}
