// Command atomyx is the CLI binary: it parses argv, routes to a
// module command, and runs it against real connected devices.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"dev.atomyx/internal/cli"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
)

func main() {
	procRunner := process.NewExec()
	clock := orchestra.SystemClock{}
	logger := logging.FromEnv()
	// list-devices renders devices for a human to read, so it scans
	// iOS through IosBootedSimulatorProbe rather than IosProbe: the
	// same probe the sidecar uses for device selection would filter by
	// simulator availability, append a runtime-label suffix to each
	// simulator's name, and reorder booted simulators first, none of
	// which a plain device listing should do.
	probe := device.NewCompositeProbe(
		logger,
		device.NewAndroidAdbProbe(procRunner),
		device.NewIosBootedSimulatorProbe(procRunner),
	)
	factory := device.NewDriverFactory(device.DriverFactoryDeps{Runner: procRunner, Clock: clock, Logger: logger})

	deps := cli.Deps{
		Probe:   probe,
		Factory: factory,
		Clock:   clock,
		Logger:  logger,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Debug:   os.Getenv("ATOMYX_DEBUG") == "1",
	}

	// A plain cli.Run would leave Ctrl-C during a run or suite command
	// killing the process immediately, skipping every deferred driver
	// Disconnect and adb-forward, XCUITest, or netcapture teardown a
	// command has already started. Trapping SIGINT and SIGTERM here
	// and threading the cancelable context through RunContext instead
	// lets those defers run on the way out.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// stop deliberately does not run before this os.Exit: the process
	// is terminating either way, and stop only releases in-process
	// signal and context bookkeeping the OS reclaims regardless.
	//nolint:gocritic // see the comment above
	os.Exit(cli.RunContext(ctx, os.Args[1:], deps))
}
