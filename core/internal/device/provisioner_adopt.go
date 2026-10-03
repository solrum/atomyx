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

// adopt decides whether the runner recorded for udid can serve the
// caller and, when it can, makes this process its owner. It returns the
// rewritten record. A runner is adopted only when all of these hold:
//
//   - the record names udid and a simulator runner
//   - the host has not rebooted and the recorded process is still the
//     recorded runner
//   - the runner answers on the recorded port with the recorded launch id
//   - the record names the compiled product that is on disk now
//
// The recorded owner is no obstacle, even when it is another live
// process, because holding the device lease is what entitles a process
// to drive the runner. The adopter rewrites the record's owner to
// itself, so the previous owner's keeper leaves the runner running.
//
// The caller must hold the device lease, because only then does no
// other process drive the runner.
func (p *DriverProvisioner) adopt(ctx context.Context, udid string, cfg iosdriver.LaunchConfig) (RunnerState, bool) {
	path := p.statePath(udid)
	if p.deps.Reaper == nil || path == "" {
		return RunnerState{}, false
	}
	state, err := ReadRunnerState(path)
	if err != nil {
		return RunnerState{}, false
	}
	if reason := p.adoptionBlocker(ctx, state, udid, cfg); reason != "" {
		p.logger.Info("not adopting the recorded runner", logging.LogContext{"udid": udid, "reason": reason})
		return RunnerState{}, false
	}
	owner, err := p.ownOwner()
	if err != nil {
		p.logger.Warn("not adopting the recorded runner", logging.LogContext{"udid": udid, "error": err.Error()})
		return RunnerState{}, false
	}
	state.OwnerPid, state.OwnerStartTime = owner.Pid, owner.StartTime
	if err := WriteRunnerState(path, state); err != nil {
		p.logger.Warn("not adopting the recorded runner", logging.LogContext{"udid": udid, "error": err.Error()})
		return RunnerState{}, false
	}
	p.logger.Info("adopting the recorded runner", logging.LogContext{"udid": udid, "port": state.HostPort})
	return state, true
}

// adoptionBlocker returns why state cannot be adopted, or an empty string.
func (p *DriverProvisioner) adoptionBlocker(ctx context.Context, state RunnerState, udid string, cfg iosdriver.LaunchConfig) string {
	if reason := p.deps.Reaper.AdoptionBlocker(state, udid); reason != "" {
		return reason
	}
	want := iosdriver.ExpectedRunner{UDID: udid, Kind: iosdriver.KindSimulator, RunnerID: state.RunnerID}
	found, _, idErr := iosdriver.CheckRunnerIdentity(ctx, state.HostPort, iosdriver.RunnerProbeTimeout, want)
	if !found || idErr != nil {
		return "the recorded runner does not answer with its launch id"
	}
	current, ok := iosdriver.NewXctestLauncher(p.runner, p.background, p.clock, cfg).CurrentBuildProduct()
	if !ok || !state.BuildProduct().Matches(current) {
		return "the runner was started from a different build of the runner product"
	}
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

// endAdopted stops the adopted runner through the reaper's guarded path,
// unless another process now owns it.
func (p *DriverProvisioner) endAdopted(ctx context.Context, held launchRecord, udid string) error {
	if held.ownedByAnotherProcess() {
		return nil
	}
	_, err := p.deps.Reaper.Reap(ctx, held.path, udid)
	return err
}

// watchAdopted reports through lost once when the process of the adopted
// runner state names is gone or has been replaced by another process.
// It returns when ctx ends.
func (p *DriverProvisioner) watchAdopted(ctx context.Context, state RunnerState, lost func(error)) {
	for {
		if err := p.awaitWatchTick(ctx); err != nil {
			return
		}
		if p.runnerProcessEnded(state) {
			lost(fmt.Errorf("the adopted runner process %d is no longer running", state.Pid))
			return
		}
	}
}

func (p *DriverProvisioner) runnerProcessEnded(state RunnerState) bool {
	info, err := p.inspector.Inspect(state.Pid)
	if errors.Is(err, process.ErrProcessNotFound) {
		return true
	}
	return err == nil && absDuration(info.StartTime.Sub(state.ProcessStartTime)) > startTimeTolerance
}

func (p *DriverProvisioner) defaultWatchTick(ctx context.Context) error {
	return p.clock.Sleep(ctx, adoptedRunnerWatchPeriod.Milliseconds())
}

// adoptedAgent registers the adopted runner described by state with the
// keeper and the watcher, and returns the agent that stops it. setEnded
// receives the stop function before the watcher can report a loss.
func (p *DriverProvisioner) adoptedAgent(ctx context.Context, state RunnerState, held launchRecord, udid string, onLost func(error), setEnded func(stop func() error)) IOSAgent {
	// The runner outlives the call that adopted it, so watching and
	// stopping it must not depend on that call's cancellation.
	stopCtx := context.WithoutCancel(ctx)
	watchCtx, stopWatching := context.WithCancel(stopCtx)
	stop := sync.OnceValue(func() error {
		stopWatching()
		err := p.endAdopted(stopCtx, held, udid)
		if p.deps.Keeper != nil {
			p.deps.Keeper.forget(state.RunnerID)
		}
		return err
	})
	setEnded(stop)
	if p.deps.Keeper != nil {
		p.deps.Keeper.keep(held, stop)
	}
	if onLost != nil {
		go p.watchAdopted(watchCtx, state, onLost)
	}
	return IOSAgent{Port: state.HostPort, RunnerID: state.RunnerID, Adopted: true, Kill: stop}
}
