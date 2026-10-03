// Package runnerprovision builds the iOS runner provisioner the CLI and
// the MCP server wire into their driver factory, so both composition
// roots share one definition of where runner records live and how a
// device maps to its record.
package runnerprovision

import (
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
)

// New returns a DriverProvisioner that launches a runner per iOS target
// on its own port, ends the runner an earlier holder left recorded
// before each launch, and records each launched runner next to the
// device lease files. It also returns the RunnerKeeper that holds every runner
// the provisioner launches, which the caller stops through Cleanup when
// its process exits. When the runtime directory cannot be resolved, the
// provisioner still launches runners but writes no record, and a warning
// says so.
func New(runner process.Runner, background process.BackgroundRunner, clock orchestra.Clock, logger logging.Logger) (*device.DriverProvisioner, *device.RunnerKeeper) {
	keeper := device.NewRunnerKeeper()
	deps := device.DriverProvisionerDeps{
		Keeper:     keeper,
		Reaper:     device.NewRunnerReaper(device.RunnerReaperDeps{Runner: runner, Clock: clock, Logger: logger}),
		Runner:     runner,
		Background: background,
		Clock:      clock,
		Logger:     logger,
	}
	dir, err := lease.ResolveRuntimeDir(lease.DefaultEnvironment())
	if err != nil {
		logger.Warn("runner records are disabled because the runtime directory is unusable", logging.LogContext{"error": err.Error()})
	} else {
		deps.RunnerStateDir = dir
		deps.RunnerStateKey = func(udid string) string {
			return string(lease.KeyFor(string(device.PlatformIOS), udid))
		}
	}
	return device.NewDriverProvisioner(deps), keeper
}

// Cleanup returns the exit cleanup of a process: it stops every runner
// the keeper holds, then runs base when it is non-nil. The runners stop
// first so a base that hangs cannot keep them alive. It is safe to call
// more than once.
func Cleanup(keeper *device.RunnerKeeper, base func()) func() {
	return func() {
		keeper.StopAll()
		if base != nil {
			base()
		}
	}
}
