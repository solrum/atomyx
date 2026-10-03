package device

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
)

// adoptedRunnerWatchPeriod is how often the process of an adopted runner
// is checked, because an adopted runner has no child process whose exit
// the provisioner could wait on.
const adoptedRunnerWatchPeriod = 2 * time.Second

var _ AdoptingProvisioner = (*DriverProvisioner)(nil)

// adoption is a runner this process took over, with what it must stop
// and watch beyond the runner itself.
type adoption struct {
	state RunnerState
	// tunnelStop stops a tunnel this call started because the recorded one
	// was gone. It is nil for a simulator and for a recorded tunnel that
	// was adopted.
	tunnelStop func()
	// tunnelAdopted reports that a recorded tunnel was adopted, so no
	// supervisor of its own watches it.
	tunnelAdopted bool
}

// adopt decides whether the runner recorded for udid can serve the
// caller and, when it can, makes this process its owner. It returns the
// rewritten record. A runner is adopted only when all of these hold:
//
//   - the record names udid and a runner of the requested kind
//   - the host has not rebooted and the recorded process is still the
//     recorded runner
//   - the record names the compiled product that is on disk now
//   - for a physical device, the tunnel to the recorded port is the
//     recorded iproxy, still alive, or a new one can be started
//   - the runner answers on the recorded port with the recorded launch id
//
// A recorded tunnel is adopted only when its process is still the
// recorded iproxy, owned by this user and leading its own process group.
// When it is gone, or fails those checks while nothing listens on the
// port, a new tunnel starts for the recorded port and its process is
// recorded. A listener on the port that cannot be accounted for blocks
// the adoption.
//
// The recorded owner is no obstacle, even when it is another live
// process, because holding the device lease is what entitles a process
// to drive the runner. The adopter rewrites the record's owner to
// itself, so the previous owner's keeper leaves the runner running.
//
// The caller must hold the device lease, because only then does no
// other process drive the runner.
func (p *DriverProvisioner) adopt(ctx context.Context, udid string, kind IosAgentKind, cfg iosdriver.LaunchConfig, onTunnelExit func(error)) (adoption, bool) {
	path := p.statePath(udid)
	if p.deps.Reaper == nil || path == "" {
		return adoption{}, false
	}
	state, err := ReadRunnerState(path)
	if err != nil {
		return adoption{}, false
	}
	adopted := adoption{state: state}
	// A tunnel started for an adoption that then fails is stopped here, so
	// its supervisor reports no exit and the launch that follows owns the
	// port alone.
	abandon := func() {
		if adopted.tunnelStop != nil {
			adopted.tunnelStop()
		}
	}
	if reason := p.adoptionBlocker(ctx, path, &adopted, udid, kind, cfg, onTunnelExit); reason != "" {
		abandon()
		p.logger.Info("not adopting the recorded runner", logging.LogContext{"udid": udid, "reason": reason})
		return adoption{}, false
	}
	owner, err := p.ownOwner()
	if err != nil {
		abandon()
		p.logger.Warn("not adopting the recorded runner", logging.LogContext{"udid": udid, "error": err.Error()})
		return adoption{}, false
	}
	adopted.state.OwnerPid, adopted.state.OwnerStartTime = owner.Pid, owner.StartTime
	if err := WriteRunnerState(path, adopted.state); err != nil {
		abandon()
		p.logger.Warn("not adopting the recorded runner", logging.LogContext{"udid": udid, "error": err.Error()})
		return adoption{}, false
	}
	p.logger.Info("adopting the recorded runner", logging.LogContext{"udid": udid, "port": adopted.state.HostPort})
	return adopted, true
}

// adoptionBlocker returns why the recorded runner cannot be adopted, or
// an empty string. For a physical device it settles the tunnel in
// adopted before the runner is probed, because the probe goes through it.
func (p *DriverProvisioner) adoptionBlocker(ctx context.Context, path string, adopted *adoption, udid string, kind IosAgentKind, cfg iosdriver.LaunchConfig, onTunnelExit func(error)) string {
	state := adopted.state
	if reason := p.deps.Reaper.AdoptionBlocker(state, udid, kind); reason != "" {
		return reason
	}
	current, ok := iosdriver.NewXctestLauncher(p.runner, p.background, p.clock, cfg).CurrentBuildProduct()
	if !ok || !state.BuildProduct().Matches(current) {
		return "the runner was started from a different build of the runner product"
	}
	driverKind := iosdriver.KindSimulator
	if kind == IosAgentKindDevice {
		if reason := p.settleTunnel(ctx, path, adopted, udid, onTunnelExit); reason != "" {
			return reason
		}
		driverKind = iosdriver.KindDevice
	}
	want := iosdriver.ExpectedRunner{UDID: udid, Kind: driverKind, RunnerID: state.RunnerID}
	found, _, idErr := iosdriver.CheckRunnerIdentity(ctx, state.HostPort, iosdriver.RunnerProbeTimeout, want)
	if !found || idErr != nil {
		return "the recorded runner does not answer with its launch id"
	}
	return ""
}

// settleTunnel makes sure a tunnel reaches the recorded port. It adopts
// the recorded iproxy when it passes the reaper's identity checks, and
// otherwise starts a new tunnel and records its process, so every tunnel
// the adopter ends up using is recorded and stoppable.
func (p *DriverProvisioner) settleTunnel(ctx context.Context, path string, adopted *adoption, udid string, onTunnelExit func(error)) string {
	state := adopted.state
	blocked := ""
	stop, pid, err := iosdriver.StartTunnel(ctx, p.background, p.clock, iosdriver.TunnelConfig{
		UDID: udid, HostPort: state.HostPort, RunnerID: state.RunnerID, OnExit: onTunnelExit,
		Reuse: func() (int, bool) {
			blocked = p.deps.Reaper.TunnelBlocker(state)
			return state.TunnelPid, blocked == ""
		},
	})
	if err != nil {
		if blocked != "" {
			return "the tunnel on the recorded port is not the recorded one: " + blocked
		}
		return "the tunnel to the recorded port cannot be started: " + err.Error()
	}
	if stop == nil {
		adopted.tunnelAdopted = true
		return ""
	}
	adopted.tunnelStop = stop
	updated, err := p.recordTunnel(path, state, pid)
	if err != nil {
		return "the new tunnel cannot be recorded: " + err.Error()
	}
	adopted.state = updated
	return ""
}

// ownOwner identifies this process as the owner of a runner.
func (p *DriverProvisioner) ownOwner() (RunnerOwner, error) {
	pid := p.ownPid()
	info, err := p.inspector.Inspect(pid)
	if err != nil {
		return RunnerOwner{}, fmt.Errorf("identify this process as the runner owner: %w", err)
	}
	return RunnerOwner{Pid: pid, StartTime: info.StartTime}, nil
}

// endAdopted stops the adopted runner and its tunnel through the
// reaper's guarded path, unless another process now owns them. A tunnel
// this process started is stopped first, so its supervisor reports no
// exit.
//
// It acts on the state this process adopted, never on the record on disk
// now, so a runner another process launched since is never signaled. The
// record is removed only while it still names this runner and this owner.
func (p *DriverProvisioner) endAdopted(ctx context.Context, a adoption, held launchRecord) error {
	if held.ownedByAnotherProcess() {
		return nil
	}
	if a.tunnelStop != nil {
		a.tunnelStop()
	}
	_, err := p.deps.Reaper.ReapState(ctx, a.state)
	if err != nil {
		return err
	}
	if held.path != "" {
		removeRunnerStateOf(held.path, a.state.RunnerID, held.owner)
	}
	return nil
}

// watchAdopted reports through lostRunner once when the process of the
// adopted runner a names is gone or has been replaced by another process,
// and through lostTunnel once when an adopted tunnel's process is. It
// returns when ctx ends or after a report.
func (p *DriverProvisioner) watchAdopted(ctx context.Context, a adoption, lostRunner, lostTunnel func(error)) {
	state := a.state
	for {
		if err := p.awaitWatchTick(ctx); err != nil {
			return
		}
		if p.processEnded(state.Pid, state.ProcessStartTime) {
			lostRunner(fmt.Errorf("the adopted runner process %d is no longer running", state.Pid))
			return
		}
		if a.tunnelAdopted && p.processEnded(state.TunnelPid, state.TunnelStartTime) {
			lostTunnel(fmt.Errorf("the adopted tunnel process %d is no longer running", state.TunnelPid))
			return
		}
	}
}

func (p *DriverProvisioner) processEnded(pid int, startedAt time.Time) bool {
	info, err := p.inspector.Inspect(pid)
	if errors.Is(err, process.ErrProcessNotFound) {
		return true
	}
	return err == nil && absDuration(info.StartTime.Sub(startedAt)) > startTimeTolerance
}

func (p *DriverProvisioner) defaultWatchTick(ctx context.Context) error {
	return p.clock.Sleep(ctx, adoptedRunnerWatchPeriod.Milliseconds())
}

// adoptedAgent registers the adopted runner described by a with the
// keeper and the watcher, and returns the agent that stops it. setEnded
// receives the stop function before the watcher can report a loss.
func (p *DriverProvisioner) adoptedAgent(ctx context.Context, a adoption, held launchRecord, udid string, onLost, onTunnelLost func(error), setEnded func(stop func() error)) IOSAgent {
	state := a.state
	// The runner outlives the call that adopted it, so watching and
	// stopping it must not depend on that call's cancellation.
	stopCtx := context.WithoutCancel(ctx)
	watchCtx, stopWatching := context.WithCancel(stopCtx)
	var kept *keptRunner
	stop := sync.OnceValue(func() error {
		stopWatching()
		err := p.endAdopted(stopCtx, a, held)
		if p.deps.Keeper != nil {
			p.deps.Keeper.forget(kept)
		}
		return err
	})
	setEnded(stop)
	if p.deps.Keeper != nil {
		kept = p.deps.Keeper.keep(held, stop)
	}
	if onLost != nil {
		go p.watchAdopted(watchCtx, a, onLost, onTunnelLost)
	}
	return IOSAgent{Port: state.HostPort, RunnerID: state.RunnerID, Adopted: true, Kill: stop}
}
