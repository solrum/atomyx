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
// on its own port and records each launched runner next to the device
// lease files. When the runtime directory cannot be resolved, the
// provisioner still launches runners but writes no record, and a warning
// says so.
func New(runner process.Runner, background process.BackgroundRunner, clock orchestra.Clock, logger logging.Logger) *device.DriverProvisioner {
	deps := device.DriverProvisionerDeps{
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
	return device.NewDriverProvisioner(deps)
}
