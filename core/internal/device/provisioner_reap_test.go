package device

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
)

const (
	orphanPid      = 5150
	orphanRunnerID = "orphan-runner"
	orphanUID      = 501
	orphanOwnPGID  = 777
)

// warnLogger collects Warn messages and drops everything else.
type warnLogger struct {
	mu    sync.Mutex
	warns []string
}

func (l *warnLogger) Debug(string, logging.LogContext) {}
func (l *warnLogger) Info(string, logging.LogContext)  {}
func (l *warnLogger) Warn(msg string, _ logging.LogContext) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, msg)
}
func (l *warnLogger) Error(string, error, logging.LogContext) {}
func (l *warnLogger) Child(logging.LogContext) logging.Logger { return l }

func (l *warnLogger) recorded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.warns...)
}

// orphanedRunner records a live xcodebuild runner for udid that a gone
// owner left behind, and returns the signaler that observes the reaper.
func (f *launchFixture) orphanedRunner(udid string) *recordingSignaler {
	f.inspector.Set(process.ProcessInfo{PID: orphanPid, PGID: orphanPid, UID: orphanUID, Command: "xcodebuild", StartTime: testProcessStart})
	state := RunnerState{
		UDID: udid, Kind: string(IosAgentKindSimulator), RunnerID: orphanRunnerID, HostPort: 53000,
		Pid: orphanPid, Pgid: orphanPid,
		ProcessStartTime: testProcessStart, HostBootTime: time.Unix(testBootSeconds, 0).UTC(),
		OwnerPid: testOwnerPid + 1, OwnerStartTime: testOwnerStart,
	}
	if err := WriteRunnerState(f.statePath(udid), state); err != nil {
		panic(err)
	}
	return &recordingSignaler{inspector: f.inspector, exitOn: map[syscall.Signal]bool{syscall.SIGTERM: true}}
}

func (f *launchFixture) reapingProvisioner(logger logging.Logger, signals ProcessGroupSignaler) *DriverProvisioner {
	p := f.provisioner(logger)
	p.deps.Reaper = NewRunnerReaper(RunnerReaperDeps{
		Inspector: f.inspector, Runner: f.runner, Clock: clock.NewFakeClock(0), Signals: signals,
		Prober:  func(context.Context, int) (iosdriver.RunnerIdentity, bool) { return iosdriver.RunnerIdentity{}, false },
		OwnPGID: orphanOwnPGID, OwnUID: orphanUID,
	})
	return p
}

func TestDriverProvisioner_EnsureIOSAgent_EndsTheRecordedOrphanBeforeLaunching(t *testing.T) {
	fx := newLaunchFixture(t)
	signals := fx.orphanedRunner("SIM-1")
	p := fx.reapingProvisioner(nil, signals)

	_, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	defer func() { _ = kill() }()

	calls := signals.recorded()
	if len(calls) != 1 || calls[0].pgid != orphanPid || calls[0].sig != syscall.SIGTERM {
		t.Fatalf("group signals = %+v, want one SIGTERM to the orphan's group %d", calls, orphanPid)
	}
	if got := len(fx.farm.started("xcodebuild")); got != 1 {
		t.Fatalf("launched runners = %d, want 1 after the orphan was ended", got)
	}
	state, err := ReadRunnerState(fx.statePath("SIM-1"))
	if err != nil || state.RunnerID == orphanRunnerID {
		t.Fatalf("record = %+v (%v), want the new launch's record in place of the orphan's", state, err)
	}
}

func TestDriverProvisioner_EnsureIOSAgent_FailureToEndTheOrphanIsLoggedAndTheLaunchProceeds(t *testing.T) {
	fx := newLaunchFixture(t)
	signals := fx.orphanedRunner("SIM-1")
	signals.failWith = map[int]error{orphanPid: errors.New("operation not permitted")}
	logger := &warnLogger{}
	p := fx.reapingProvisioner(logger, signals)

	_, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v, want a reap failure logged but not fatal", err)
	}
	defer func() { _ = kill() }()

	if warns := logger.recorded(); len(warns) != 1 || warns[0] != "could not end an orphaned runner" {
		t.Fatalf("warnings = %v, want the reap failure reported once", warns)
	}
}

func TestDriverProvisioner_EnsureIOSAgent_FixedPortModeNeverReaps(t *testing.T) {
	fx := newLaunchFixture(t)
	signals := fx.orphanedRunner("SIM-1")
	p := fx.reapingProvisioner(nil, signals)
	p.deps.FixedIosPort = true

	_, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	defer func() { _ = kill() }()

	if calls := signals.recorded(); len(calls) != 0 {
		t.Fatalf("group signals = %+v, want none in fixed port mode", calls)
	}
	if _, err := os.Stat(fx.statePath("SIM-1")); err != nil {
		t.Fatalf("record: %v, want the fixed port mode to leave it alone", err)
	}
}
