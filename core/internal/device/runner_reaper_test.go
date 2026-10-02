package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

const (
	reapTestUDID  = "SIM-REAP"
	reapTestPid   = 4321
	reapTestOwnPG = 777
	reapTestUID   = 501
)

var (
	reapTestBoot  = time.Unix(1_759_372_800, 0).UTC()
	reapTestStart = time.Unix(1_759_400_000, 500_000_000).UTC()
)

type signalCall struct {
	pgid int
	sig  syscall.Signal
}

// recordingSignaler records every group signal. A signal named in
// exitOn removes the group leader from the inspector, which models a
// process that dies on that signal.
type recordingSignaler struct {
	mu        sync.Mutex
	calls     []signalCall
	inspector *process.FakeInspector
	pid       int
	exitOn    map[syscall.Signal]bool
}

func (s *recordingSignaler) SignalGroup(pgid int, sig syscall.Signal) error {
	s.mu.Lock()
	s.calls = append(s.calls, signalCall{pgid: pgid, sig: sig})
	s.mu.Unlock()
	if s.exitOn[sig] {
		s.inspector.Remove(s.pid)
	}
	return nil
}

func (s *recordingSignaler) recorded() []signalCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]signalCall(nil), s.calls...)
}

type reapFixture struct {
	t         *testing.T
	path      string
	inspector *process.FakeInspector
	signals   *recordingSignaler
	runner    *process.Fake
	answering func() (iosdriver.RunnerIdentity, bool)
	reaper    *RunnerReaper
	state     RunnerState
}

func newReapFixture(t *testing.T, kind IosAgentKind) *reapFixture {
	t.Helper()
	f := &reapFixture{t: t, path: filepath.Join(t.TempDir(), "key.ios-runner.json")}
	f.inspector = process.NewFakeInspector(reapTestBoot)
	f.inspector.Set(process.ProcessInfo{
		PID: reapTestPid, PGID: reapTestPid, UID: reapTestUID, Command: "xcodebuild", StartTime: reapTestStart,
	})
	f.signals = &recordingSignaler{inspector: f.inspector, pid: reapTestPid, exitOn: map[syscall.Signal]bool{syscall.SIGTERM: true}}
	f.runner = process.NewFake()
	f.answering = func() (iosdriver.RunnerIdentity, bool) { return iosdriver.RunnerIdentity{}, false }
	f.reaper = NewRunnerReaper(RunnerReaperDeps{
		Inspector: f.inspector,
		Runner:    f.runner,
		Clock:     clock.NewFakeClock(0),
		Signals:   f.signals,
		Prober: func(context.Context, int) (iosdriver.RunnerIdentity, bool) {
			return f.answering()
		},
		GracePeriod: 5 * time.Second,
		OwnPGID:     reapTestOwnPG,
		OwnUID:      reapTestUID,
	})
	f.state = RunnerState{
		UDID: reapTestUDID, Kind: string(kind), RunnerID: "runner-1", HostPort: 53000,
		Pid: reapTestPid, Pgid: reapTestPid,
		ProcessStartTime: reapTestStart, HostBootTime: reapTestBoot, StartedAt: reapTestBoot,
	}
	return f
}

func (f *reapFixture) write() {
	f.t.Helper()
	if err := WriteRunnerState(f.path, f.state); err != nil {
		f.t.Fatal(err)
	}
}

func (f *reapFixture) reap() ReapOutcome {
	f.t.Helper()
	out, err := f.reaper.Reap(context.Background(), f.path, reapTestUDID)
	if err != nil {
		f.t.Fatalf("Reap() error = %v", err)
	}
	return out
}

func (f *reapFixture) assertRecordRemoved() {
	f.t.Helper()
	if _, err := os.Stat(f.path); !errors.Is(err, os.ErrNotExist) {
		f.t.Fatalf("runner record stat error = %v, want it removed", err)
	}
}

func (f *reapFixture) assertNoSignal() {
	f.t.Helper()
	if got := f.signals.recorded(); len(got) != 0 {
		f.t.Fatalf("signals = %+v, want none", got)
	}
}

func TestReap_NoRecordDoesNothing(t *testing.T) {
	f := newReapFixture(t, IosAgentKindSimulator)
	if out := f.reap(); out.Found || out.Signaled {
		t.Fatalf("Reap() = %+v, want nothing found", out)
	}
	f.assertNoSignal()
}

func TestReap_LiveOrphanGetsSIGTERMToItsGroupAndTheRecordIsRemoved(t *testing.T) {
	f := newReapFixture(t, IosAgentKindDevice)
	f.write()

	out := f.reap()

	if !out.Signaled || out.Killed || out.PGID != reapTestPid {
		t.Fatalf("Reap() = %+v, want signaled, not killed, pgid %d", out, reapTestPid)
	}
	got := f.signals.recorded()
	if len(got) != 1 || got[0] != (signalCall{pgid: reapTestPid, sig: syscall.SIGTERM}) {
		t.Fatalf("signals = %+v, want one SIGTERM to the recorded group", got)
	}
	f.assertRecordRemoved()
}

func TestReap_AGroupThatIgnoresSIGTERMGetsSIGKILLAfterTheGracePeriod(t *testing.T) {
	f := newReapFixture(t, IosAgentKindDevice)
	f.signals.exitOn = map[syscall.Signal]bool{syscall.SIGKILL: true}
	f.write()

	out := f.reap()

	if !out.Killed {
		t.Fatalf("Reap() = %+v, want Killed", out)
	}
	got := f.signals.recorded()
	if len(got) != 2 || got[0].sig != syscall.SIGTERM || got[1].sig != syscall.SIGKILL {
		t.Fatalf("signals = %+v, want SIGTERM then SIGKILL", got)
	}
	f.assertRecordRemoved()
}

func TestReap_AGroupThatSurvivesSIGKILLReportsAnErrorAndKeepsTheRecord(t *testing.T) {
	f := newReapFixture(t, IosAgentKindDevice)
	f.signals.exitOn = nil
	f.write()

	_, err := f.reaper.Reap(context.Background(), f.path, reapTestUDID)

	if err == nil || !strings.Contains(err.Error(), "did not exit") {
		t.Fatalf("Reap() error = %v, want the survivor reported", err)
	}
	if _, statErr := os.Stat(f.path); statErr != nil {
		t.Fatalf("record stat = %v, want the record kept for the next acquirer", statErr)
	}
}

func TestReap_RecordsItMustNotActOnAreDiscardedWithoutSignaling(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(f *reapFixture)
		reason string
	}{
		{"boot time moved past the tolerance", func(f *reapFixture) { f.state.HostBootTime = reapTestBoot.Add(-time.Hour) }, "rebooted"},
		{"another device", func(f *reapFixture) { f.state.UDID = "OTHER" }, "another device"},
		{"no udid recorded", func(f *reapFixture) { f.state.UDID = "" }, "no target"},
		{"no kind recorded", func(f *reapFixture) { f.state.Kind = "" }, "no target"},
		{"pgid one", func(f *reapFixture) { f.state.Pgid = 1 }, "no signalable"},
		{"pgid zero", func(f *reapFixture) { f.state.Pgid = 0 }, "no signalable"},
		{"our own group", func(f *reapFixture) { f.state.Pgid = reapTestOwnPG }, "own group"},
		{"pid reused by a process started later", func(f *reapFixture) {
			f.inspector.Set(process.ProcessInfo{PID: reapTestPid, PGID: reapTestPid, UID: reapTestUID, Command: "xcodebuild", StartTime: reapTestStart.Add(time.Minute)})
		}, "different process"},
		{"another user's process", func(f *reapFixture) {
			f.inspector.Set(process.ProcessInfo{PID: reapTestPid, PGID: reapTestPid, UID: reapTestUID + 1, Command: "xcodebuild", StartTime: reapTestStart})
		}, "another user"},
		{"not xcodebuild", func(f *reapFixture) {
			f.inspector.Set(process.ProcessInfo{PID: reapTestPid, PGID: reapTestPid, UID: reapTestUID, Command: "node", StartTime: reapTestStart})
		}, "not xcodebuild"},
		{"left the recorded group", func(f *reapFixture) {
			f.inspector.Set(process.ProcessInfo{PID: reapTestPid, PGID: 99, UID: reapTestUID, Command: "xcodebuild", StartTime: reapTestStart})
		}, "left the recorded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReapFixture(t, IosAgentKindDevice)
			tc.mutate(f)
			f.write()

			out := f.reap()

			if out.Signaled || !strings.Contains(out.Discarded, tc.reason) {
				t.Fatalf("Reap() = %+v, want a discard mentioning %q and no signal", out, tc.reason)
			}
			f.assertNoSignal()
			f.assertRecordRemoved()
		})
	}
}

func TestReap_BootTimeWithinTheToleranceStillReapsTheOrphan(t *testing.T) {
	f := newReapFixture(t, IosAgentKindDevice)
	f.state.HostBootTime = reapTestBoot.Add(-3 * time.Second)
	f.write()

	if out := f.reap(); !out.Signaled {
		t.Fatalf("Reap() = %+v, want the orphan reaped despite a 3 second boot time drift", out)
	}
}

func TestReap_AnUndecodableRecordIsDiscarded(t *testing.T) {
	f := newReapFixture(t, IosAgentKindDevice)
	if err := os.WriteFile(f.path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := f.reap()

	if !out.Found || !strings.Contains(out.Discarded, "cannot be decoded") {
		t.Fatalf("Reap() = %+v, want an undecodable discard", out)
	}
	f.assertNoSignal()
	f.assertRecordRemoved()
}

func TestReap_ADeadProcessIsNotSignaledAndTheRecordIsRemoved(t *testing.T) {
	f := newReapFixture(t, IosAgentKindDevice)
	f.inspector.Remove(reapTestPid)
	f.write()

	out := f.reap()

	if out.Signaled || out.Discarded != "" {
		t.Fatalf("Reap() = %+v, want no signal and no discard for an already dead runner", out)
	}
	f.assertNoSignal()
	f.assertRecordRemoved()
}

func TestReap_ASimulatorRunnerOutsideTheGroupIsTerminatedThroughSimctl(t *testing.T) {
	f := newReapFixture(t, IosAgentKindSimulator)
	terminated := false
	f.runner.Stub(process.ExactMatch("xcrun", "simctl", "terminate", reapTestUDID, DefaultRunnerBundleID), process.Result{}, nil)
	f.answering = func() (iosdriver.RunnerIdentity, bool) {
		if len(f.runner.Invocations()) > 0 {
			terminated = true
			return iosdriver.RunnerIdentity{}, false
		}
		return iosdriver.RunnerIdentity{RunnerID: "runner-1", SimulatorUDID: reapTestUDID}, true
	}
	f.write()

	out := f.reap()

	if !out.TerminatedViaSimctl || !terminated {
		t.Fatalf("Reap() = %+v, want the surviving simulator runner terminated through simctl", out)
	}
	f.assertRecordRemoved()
}

func TestReap_ASimulatorPortAnsweredByAnotherRunnerIsLeftAlone(t *testing.T) {
	f := newReapFixture(t, IosAgentKindSimulator)
	f.answering = func() (iosdriver.RunnerIdentity, bool) {
		return iosdriver.RunnerIdentity{RunnerID: "someone-else", SimulatorUDID: reapTestUDID}, true
	}
	f.write()

	out := f.reap()

	if out.TerminatedViaSimctl || len(f.runner.Invocations()) != 0 {
		t.Fatalf("Reap() = %+v, invocations = %+v, want no simctl for a different runner", out, f.runner.Invocations())
	}
}

func TestReap_ASimulatorRunnerThatStillAnswersAfterSimctlIsReportedAndTheRecordKept(t *testing.T) {
	f := newReapFixture(t, IosAgentKindSimulator)
	f.answering = func() (iosdriver.RunnerIdentity, bool) {
		return iosdriver.RunnerIdentity{RunnerID: "runner-1", SimulatorUDID: reapTestUDID}, true
	}
	f.write()

	_, err := f.reaper.Reap(context.Background(), f.path, reapTestUDID)

	if err == nil || !strings.Contains(err.Error(), "still answers") {
		t.Fatalf("Reap() error = %v, want the survivor reported", err)
	}
	if _, statErr := os.Stat(f.path); statErr != nil {
		t.Fatalf("record stat = %v, want the record kept", statErr)
	}
}
