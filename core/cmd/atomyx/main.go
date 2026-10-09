// Command atomyx is the CLI binary: it parses argv, routes to a
// module command, and runs it against real connected devices.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"dev.atomyx/cmd/internal/browseropen"
	"dev.atomyx/cmd/internal/runnerprovision"
	"dev.atomyx/cmd/internal/sessionwire"
	"dev.atomyx/internal/cli"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/shutdown"
)

func main() {
	procRunner := process.NewExec()
	background := process.NewExecBackground()
	clock := orchestra.SystemClock{}
	logger := logging.FromEnv()
	// This scan backs list-devices and also resolves an iOS --device
	// udid to a simulator or a physical device. list-devices renders
	// devices for a human to read, so it scans
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
	provisioner, keeper := runnerprovision.New(procRunner, background, clock, logger)
	factory := device.NewDriverFactory(device.DriverFactoryDeps{Runner: procRunner, Background: background, Clock: clock, Logger: logger, Provisioner: provisioner, OwnAndroidForwards: true})
	recorder := device.NewScreenRecorder(device.RecorderDeps{Runner: procRunner, Background: background})

	sessionDeps, err := sessionwire.Deps(procRunner, clock, logger, "", "cli")
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomyx: %v\n", err)
		os.Exit(1)
	}
	sessionDeps.Preparer = provisioner

	deps := cli.Deps{
		Probe:    probe,
		Runners:  device.PingRunnerIdentifier{},
		Factory:  factory,
		Clock:    clock,
		Logger:   logger,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Browser:  browseropen.New(procRunner, runtime.GOOS),
		Stdin:    os.Stdin,
		Session:  sessionDeps,
		Recorder: recorder,
		Timeouts: sessionwire.ResolveTimeouts(sessionwire.LoadConfigBestEffort(logger), os.Getenv, logger),
		HostCommand: func(ctx context.Context, name string, args ...string) (string, error) {
			res, err := procRunner.Run(ctx, name, args...)
			return res.Stdout + res.Stderr, err
		},
		Debug: os.Getenv("ATOMYX_DEBUG") == "1",
	}
	deps.StdinTerminal = stdinIsTerminal()

	// A plain cli.Run would leave Ctrl-C during a run or suite command
	// killing the process immediately, skipping every deferred driver
	// Disconnect and adb-forward, XCUITest, or netcapture teardown a
	// command has already started. Trapping SIGINT and SIGTERM here
	// and threading the cancelable context through RunContext instead
	// lets those defers run on the way out. The command writes its
	// report and flushes its logs before RunContext returns, and the
	// watchdog only fires once the whole grace period has lapsed, so
	// a flush that finishes in time is never cut short.
	ctx, cancel := context.WithCancelCause(context.Background())
	sigCh := make(chan os.Signal, 1)
	cleanup := runnerprovision.Cleanup(keeper, nil)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		shutdown.HandleStop(clock, sig, shutdown.StopHooks{Cancel: func() { cancel(shutdown.InterruptCause(sig)) }, Cleanup: cleanup, Exit: os.Exit})
	}()

	// cancel deliberately does not run before this os.Exit: the process
	// is terminating either way, and cancel only releases in-process
	// context bookkeeping the OS reclaims regardless.
	code := cli.RunContext(ctx, os.Args[1:], deps)
	// Closing a session leaves its runner running, so the process stops
	// the runners it owns itself before it exits.
	if !shutdown.Bounded(ctx, clock, shutdown.GracePeriod, cleanup) {
		fmt.Fprintln(os.Stderr, "atomyx: runner cleanup did not finish in time")
	}
	//nolint:gocritic // see the comment above
	os.Exit(code)
}

// stdinIsTerminal reports whether standard input is an interactive
// terminal rather than a pipe or a file.
func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
