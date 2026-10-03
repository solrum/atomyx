package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
)

const (
	// bootTimeTolerance is how far the recorded host boot time may differ
	// from the current one before the record is considered to predate a
	// reboot. The kernel's boot time moves when the wall clock is stepped,
	// so an exact comparison would discard a valid record.
	bootTimeTolerance = 5 * time.Second

	// startTimeTolerance is how far the recorded process start time may
	// differ from the live one before the pid is considered reused.
	startTimeTolerance = 2 * time.Second

	// reapPollInterval is the pause between checks that a signaled
	// process group has gone.
	reapPollInterval = 100 * time.Millisecond

	// reapProbeTimeout bounds one ping of the recorded runner port.
	reapProbeTimeout = iosdriver.RunnerProbeTimeout

	// reapCommand is the process name of a runner group the reaper signals,
	// because the record names an xcodebuild process group.
	reapCommand = "xcodebuild"

	// reapTunnelCommand is the only other process name the reaper signals,
	// because the record names the iproxy tunnel of a physical device.
	reapTunnelCommand = "iproxy"

	// DefaultRunnerBundleID is the bundle id of the XCUITest runner app
	// the launcher installs on a simulator, which survives its
	// xcodebuild process.
	DefaultRunnerBundleID = "dev.atomyx.driver.host.xctrunner"
)

// ReapOutcome describes what a reap did with the record it found.
type ReapOutcome struct {
	// Found reports that a runner record existed.
	Found bool
	// Discarded names why a record was removed without signaling any
	// process, and is empty when the record named a live orphan or no
	// record existed.
	Discarded string
	// Signaled reports that the recorded process group was sent a
	// signal.
	Signaled bool
	// PGID is the process group the record named.
	PGID int
	// TunnelSignaled reports that the recorded iproxy tunnel was sent a
	// signal.
	TunnelSignaled bool
	// TunnelKilled reports that the tunnel needed SIGKILL after SIGTERM
	// and the grace period.
	TunnelKilled bool
	// Killed reports that the group needed SIGKILL after SIGTERM and the
	// grace period.
	Killed bool
	// TerminatedViaSimctl reports that a simulator runner outlived its
	// xcodebuild group and was ended with simctl.
	TerminatedViaSimctl bool
}

// ProcessGroupSignaler delivers a signal to every process of a group.
type ProcessGroupSignaler interface {
	SignalGroup(pgid int, sig syscall.Signal) error
}

type systemSignaler struct{}

func (systemSignaler) SignalGroup(pgid int, sig syscall.Signal) error {
	return syscall.Kill(-pgid, sig)
}

// RunnerProber asks whatever answers on a host port for its runner
// identity.
type RunnerProber func(ctx context.Context, port int) (iosdriver.RunnerIdentity, bool)

// RunnerReaperDeps configures a RunnerReaper. Every field except
// Inspector and Runner has a production default.
type RunnerReaperDeps struct {
	Inspector process.Inspector
	// Runner executes simctl for a simulator runner that outlived its
	// process group.
	Runner  process.Runner
	Clock   clock.Clock
	Signals ProcessGroupSignaler
	Prober  RunnerProber
	Logger  logging.Logger
	// GracePeriod is how long a group gets between SIGTERM and SIGKILL,
	// and how long a simulator runner gets to stop answering. Zero means
	// five seconds.
	GracePeriod time.Duration
	// OwnPGID and OwnUID identify the calling process. Zero means the
	// running process.
	OwnPGID int
	OwnUID  int
	// RunnerBundleID overrides DefaultRunnerBundleID.
	RunnerBundleID string
}

// RunnerReaper ends an iOS runner a crashed Atomyx process left
// behind. It signals a process group only after proving the recorded
// pid is still the process that was recorded, so a reused pid is never
// killed.
type RunnerReaper struct {
	deps RunnerReaperDeps
}

const defaultReapGrace = 5 * time.Second

// NewRunnerReaper constructs a RunnerReaper from deps.
func NewRunnerReaper(deps RunnerReaperDeps) *RunnerReaper {
	if deps.Inspector == nil {
		deps.Inspector = process.SystemInspector{}
	}
	if deps.Clock == nil {
		deps.Clock = clock.SystemClock{}
	}
	if deps.Signals == nil {
		deps.Signals = systemSignaler{}
	}
	if deps.Prober == nil {
		deps.Prober = func(ctx context.Context, port int) (iosdriver.RunnerIdentity, bool) {
			return iosdriver.ProbeRunnerIdentity(ctx, port, reapProbeTimeout)
		}
	}
	if deps.Logger == nil {
		deps.Logger = logging.NoopLogger{}
	}
	if deps.GracePeriod == 0 {
		deps.GracePeriod = defaultReapGrace
	}
	if deps.OwnPGID == 0 {
		deps.OwnPGID = process.OwnProcessGroup()
	}
	if deps.OwnUID == 0 {
		deps.OwnUID = process.OwnUID()
	}
	if deps.RunnerBundleID == "" {
		deps.RunnerBundleID = DefaultRunnerBundleID
	}
	return &RunnerReaper{deps: deps}
}

// Reap inspects the runner record at statePath for the device udid and
// ends the runner it names when that runner is an orphan. The caller
// must hold the device lease and have found it free, because only then
// is a recorded runner known to belong to a process that no longer
// exists.
//
// A record that cannot be decoded, that names another device, that
// predates a reboot, or whose pid is gone or now belongs to another
// process, is removed without signaling anything. A record that names
// the live xcodebuild process group it recorded is sent SIGTERM, then
// SIGKILL after the grace period. The iproxy tunnel the record names is
// ended the same way once it passes the same checks: still the recorded
// process, owned by the caller, leading its own group, and an iproxy
// command. A tunnel that fails a check is left alone and the runner is
// handled as usual. For a simulator, a runner that still
// answers its recorded port with its recorded launch id after the group
// is gone is ended with simctl.
//
// The returned error reports a runner that could not be ended. The
// record stays in that case, so the next acquirer tries again.
func (r *RunnerReaper) Reap(ctx context.Context, statePath, udid string) (ReapOutcome, error) {
	state, err := ReadRunnerState(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return ReapOutcome{}, nil
	}
	if err != nil {
		return r.discard(statePath, "the runner record cannot be decoded"), nil
	}
	outcome := ReapOutcome{Found: true, PGID: state.Pgid}
	if reason := r.unusableRecord(state, udid); reason != "" {
		return r.discard(statePath, reason), nil
	}
	tunnelSignaled, tunnelKilled, err := r.reapTunnel(ctx, state)
	outcome.TunnelSignaled, outcome.TunnelKilled = tunnelSignaled, tunnelKilled
	if err != nil {
		return outcome, err
	}
	alive, reason := r.isRecordedRunner(state)
	if reason != "" {
		discarded := r.discard(statePath, reason)
		discarded.TunnelSignaled, discarded.TunnelKilled = tunnelSignaled, tunnelKilled
		return discarded, nil
	}
	if alive {
		outcome.Signaled = true
		killed, err := r.endGroup(ctx, state.Pid, state.Pgid)
		outcome.Killed = killed
		if err != nil {
			return outcome, err
		}
	}
	if state.Kind == string(IosAgentKindSimulator) {
		terminated, err := r.endSurvivingSimulatorRunner(ctx, state)
		outcome.TerminatedViaSimctl = terminated
		if err != nil {
			return outcome, err
		}
	}
	_ = os.Remove(statePath)
	return outcome, nil
}

// AdoptionBlocker reports why the runner a record names cannot be adopted
// by a new holder of udid, or an empty string when it can. It applies the
// checks Reap applies before it signals anything: the record names this
// device, the host has not rebooted since it was written, and the
// recorded pid is a live process that is still the recorded xcodebuild
// runner. It adds that the runner is a simulator runner with a launch id,
// because only that runner can be proven the same one later.
func (r *RunnerReaper) AdoptionBlocker(state RunnerState, udid string) string {
	if reason := r.unusableRecord(state, udid); reason != "" {
		return reason
	}
	if state.Kind != string(IosAgentKindSimulator) {
		return "the recorded runner is not a simulator runner"
	}
	if state.RunnerID == "" || state.HostPort <= 0 {
		return "the runner record carries no launch id or port"
	}
	alive, reason := r.isRecordedRunner(state)
	if reason != "" {
		return reason
	}
	if !alive {
		return "the recorded runner process has gone"
	}
	return ""
}

func (r *RunnerReaper) discard(statePath, reason string) ReapOutcome {
	_ = os.Remove(statePath)
	r.deps.Logger.Warn("discarded a runner record without signaling", logging.LogContext{"reason": reason})
	return ReapOutcome{Found: true, Discarded: reason}
}

// unusableRecord reports why a decoded record cannot be trusted to name
// a process to signal, or an empty string when it can.
func (r *RunnerReaper) unusableRecord(state RunnerState, udid string) string {
	if state.UDID == "" || state.Kind == "" {
		return "the runner record carries no target"
	}
	if !strings.EqualFold(state.UDID, udid) {
		return "the runner record belongs to another device"
	}
	boot, err := r.deps.Inspector.BootTime()
	if err != nil {
		return "the host boot time cannot be read"
	}
	if absDuration(boot.Sub(state.HostBootTime)) > bootTimeTolerance {
		return "the host rebooted after the runner was recorded"
	}
	if state.Pid <= 1 || state.Pgid <= 1 {
		return "the runner record names no signalable process group"
	}
	if state.Pgid == r.deps.OwnPGID {
		return "the runner record names this process's own group"
	}
	return ""
}

// isRecordedRunner reports whether the recorded pid is a live process
// that is still the recorded xcodebuild runner. A non-empty reason
// means the record is stale and must not be acted on. alive false with
// an empty reason means the process has already gone.
func (r *RunnerReaper) isRecordedRunner(state RunnerState) (alive bool, reason string) {
	info, err := r.deps.Inspector.Inspect(state.Pid)
	if errors.Is(err, process.ErrProcessNotFound) {
		return false, ""
	}
	if err != nil {
		return false, "the recorded process cannot be inspected"
	}
	switch {
	case absDuration(info.StartTime.Sub(state.ProcessStartTime)) > startTimeTolerance:
		return false, "the recorded pid now belongs to a different process"
	case info.UID != r.deps.OwnUID:
		return false, "the recorded process belongs to another user"
	case info.Command != reapCommand:
		return false, "the recorded process is not xcodebuild"
	case info.PGID != state.Pgid:
		return false, "the recorded process left the recorded process group"
	}
	return true, ""
}

// endGroup sends SIGTERM to the group pgid led by pid, waits for the
// leader to go, and sends SIGKILL when the grace period passes first. It
// reports whether SIGKILL was needed.
func (r *RunnerReaper) endGroup(ctx context.Context, pid, pgid int) (killed bool, err error) {
	if err := r.deps.Signals.SignalGroup(pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return false, fmt.Errorf("signal the orphaned process group %d: %w", pgid, err)
	}
	gone, err := r.waitForExit(ctx, pid)
	if err != nil || gone {
		return false, err
	}
	if err := r.deps.Signals.SignalGroup(pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return true, fmt.Errorf("kill the orphaned process group %d: %w", pgid, err)
	}
	gone, err = r.waitForExit(ctx, pid)
	if err != nil {
		return true, err
	}
	if !gone {
		return true, fmt.Errorf("the orphaned process group %d did not exit after SIGKILL", pgid)
	}
	return true, nil
}

// reapTunnel ends the iproxy tunnel the record names when it is still the
// recorded process. A tunnel that has gone, or that fails a guard, is not
// signaled.
func (r *RunnerReaper) reapTunnel(ctx context.Context, state RunnerState) (signaled, killed bool, err error) {
	if state.TunnelPid <= 1 {
		return false, false, nil
	}
	info, err := r.deps.Inspector.Inspect(state.TunnelPid)
	if errors.Is(err, process.ErrProcessNotFound) {
		return false, false, nil
	}
	if err != nil {
		r.deps.Logger.Warn("left the recorded tunnel alone", logging.LogContext{"reason": "the recorded tunnel cannot be inspected"})
		return false, false, nil
	}
	if reason := r.unusableTunnel(state, info); reason != "" {
		r.deps.Logger.Warn("left the recorded tunnel alone", logging.LogContext{"reason": reason})
		return false, false, nil
	}
	killed, err = r.endGroup(ctx, state.TunnelPid, state.TunnelPid)
	return true, killed, err
}

// unusableTunnel reports why a live process cannot be the recorded
// tunnel, or an empty string when it is.
func (r *RunnerReaper) unusableTunnel(state RunnerState, info process.ProcessInfo) string {
	switch {
	case state.TunnelPid == r.deps.OwnPGID:
		return "the recorded tunnel is this process's own group"
	case absDuration(info.StartTime.Sub(state.TunnelStartTime)) > startTimeTolerance:
		return "the recorded tunnel pid now belongs to a different process"
	case info.UID != r.deps.OwnUID:
		return "the recorded tunnel belongs to another user"
	case info.Command != reapTunnelCommand:
		return "the recorded tunnel is not iproxy"
	case info.PGID != state.TunnelPid:
		return "the recorded tunnel does not lead its own process group"
	}
	return ""
}

// waitForExit polls until pid is gone or the grace period passes, and
// reports whether it is gone.
func (r *RunnerReaper) waitForExit(ctx context.Context, pid int) (bool, error) {
	deadline := r.deps.Clock.NowMs() + r.deps.GracePeriod.Milliseconds()
	for {
		if _, err := r.deps.Inspector.Inspect(pid); errors.Is(err, process.ErrProcessNotFound) {
			return true, nil
		}
		if r.deps.Clock.NowMs() >= deadline {
			return false, nil
		}
		if err := r.deps.Clock.Sleep(ctx, reapPollInterval.Milliseconds()); err != nil {
			return false, err
		}
	}
}

// endSurvivingSimulatorRunner covers the runner app of a simulator,
// which runs under the simulator's own launchd and so outside the
// xcodebuild process group. When the recorded port still answers with
// the recorded launch id, the runner app is terminated through simctl.
func (r *RunnerReaper) endSurvivingSimulatorRunner(ctx context.Context, state RunnerState) (bool, error) {
	if !r.stillAnswers(ctx, state) {
		return false, nil
	}
	if _, err := r.deps.Runner.Run(ctx, "xcrun", "simctl", "terminate", state.UDID, r.deps.RunnerBundleID); err != nil {
		return true, fmt.Errorf("terminate the orphaned simulator runner on %s: %w", state.UDID, err)
	}
	deadline := r.deps.Clock.NowMs() + r.deps.GracePeriod.Milliseconds()
	for r.stillAnswers(ctx, state) {
		if r.deps.Clock.NowMs() >= deadline {
			return true, fmt.Errorf("the orphaned simulator runner on %s still answers port %d after simctl terminate", state.UDID, state.HostPort)
		}
		if err := r.deps.Clock.Sleep(ctx, reapPollInterval.Milliseconds()); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (r *RunnerReaper) stillAnswers(ctx context.Context, state RunnerState) bool {
	if state.HostPort <= 0 || state.RunnerID == "" {
		return false
	}
	got, ok := r.deps.Prober(ctx, state.HostPort)
	return ok && got.RunnerID == state.RunnerID
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
