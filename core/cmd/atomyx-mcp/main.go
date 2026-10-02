// Command atomyx-mcp is the Atomyx MCP server: a stdio-based process
// an AI agent connects to for tool-driven mobile test orchestration
// over the 28-tool surface in dev.atomyx/internal/mcp.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"dev.atomyx/cmd/internal/runnerprovision"
	"dev.atomyx/cmd/internal/sessionwire"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/mcp"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/shutdown"
	"dev.atomyx/internal/storage"
	"dev.atomyx/internal/version"
)

const (
	serverName    = "atomyx"
	serverVersion = "0.1.0"

	// binaryName identifies this binary on its own --version line. It
	// is distinct from serverName, which the MCP protocol handshake
	// reports and is not part of this operator-facing surface.
	binaryName = "atomyx-mcp"
)

func main() {
	if versionRequested(os.Args[1:]) {
		fmt.Println(versionLine())
		os.Exit(0)
	}
	os.Exit(run())
}

// versionRequested reports whether args asked for this binary's build
// identity instead of starting the server. atomyx-mcp has no other
// flags or subcommands, so a bare --version or -v anywhere main
// forwards it is enough.
func versionRequested(args []string) bool {
	return len(args) > 0 && (args[0] == "--version" || args[0] == "-v")
}

// versionLine is the single line atomyx-mcp reports for --version:
// its own binary name plus the build identity every Atomyx binary
// shares.
func versionLine() string {
	return fmt.Sprintf("%s %s", binaryName, version.Read())
}

// run holds every deferred cleanup so it always runs before the
// process exits: main only calls os.Exit, on run's returned code,
// after run itself has returned and every defer inside it has fired.
func run() int {
	procRunner := process.NewExec()
	background := process.NewExecBackground()
	clock := orchestra.SystemClock{}
	logger := logging.FromEnv()
	probe := device.NewCompositeProbe(
		logger,
		device.NewAndroidAdbProbe(procRunner),
		device.NewIosProbe(procRunner),
	)
	provisioner := runnerprovision.New(procRunner, background, clock, logger)
	factory := device.NewDriverFactory(device.DriverFactoryDeps{Runner: procRunner, Background: background, Clock: clock, Logger: logger, Provisioner: provisioner})

	// storage.NewFileStorage resolves its root as opts.Root, then the
	// ATOMYX_STORAGE_DIR environment variable, then "~/.atomyx". An
	// empty Root here defers entirely to that precedence.
	store, err := storage.NewFileStorage(storage.FileStorageOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", err)
		return 1
	}

	// The catalog reads versioned run folders from the same directory
	// the store resolved, and an unfinished run counts as running only
	// while this process holds it as the active run.
	runStore := storage.NewRunStore()
	runs := storage.NewRunCatalog(store, store.Root(), runStore)

	sessionDeps, err := sessionwire.Deps(procRunner, clock, logger, binaryName, "mcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", err)
		return 1
	}
	sessionDeps.Preparer = provisioner

	server := mcp.NewServer(mcp.Info{Name: serverName, Version: serverVersion}, mcp.ServerDeps{
		Probe:    probe,
		Runners:  device.PingRunnerIdentifier{},
		Factory:  factory,
		Storage:  store,
		RunStore: runStore,
		Runs:     runs,
		Clock:    clock,
		Logger:   logger,
		Session:  sessionDeps,
		Timeouts: sessionwire.ResolveTimeouts(sessionwire.LoadConfigBestEffort(logger), os.Getenv, logger),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		// Serve blocks in the stdin read and its session close waits
		// for in-flight tool calls, whose contexts are not canceled by
		// the session closing. Closing stdin fails the read so those
		// calls are canceled, and the watchdog releases the device
		// and ends the process if they still do not return in time.
		shutdown.HandleStop(clock, sig, shutdown.StopHooks{
			Cancel:     cancel,
			CloseInput: os.Stdin.Close,
			Cleanup:    server.Close,
			Exit:       os.Exit,
		})
	}()

	serveErr := mcp.Serve(ctx, server)

	// Serve returns on a stop signal and on the client closing stdin
	// alike. Either way the active device must be released here, because
	// its helper processes run in their own process group and a terminal
	// Ctrl-C does not reach them.
	if !shutdown.Bounded(clock, shutdown.GracePeriod, server.Close) {
		fmt.Fprintln(os.Stderr, "atomyx-mcp: device cleanup did not finish in time")
	}
	if serveErr != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", serveErr)
		return 1
	}
	return 0
}
