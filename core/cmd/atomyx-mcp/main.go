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

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/mcp"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
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
	clock := orchestra.SystemClock{}
	logger := logging.FromEnv()
	probe := device.NewCompositeProbe(
		logger,
		device.NewAndroidAdbProbe(procRunner),
		device.NewIosProbe(procRunner),
	)
	factory := device.NewDriverFactory(device.DriverFactoryDeps{Runner: procRunner, Clock: clock, Logger: logger})

	// storage.NewFileStorage resolves its root as opts.Root, then the
	// ATOMYX_STORAGE_DIR environment variable, then "~/.atomyx". An
	// empty Root here defers entirely to that precedence.
	store, err := storage.NewFileStorage(storage.FileStorageOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", err)
		return 1
	}

	server := mcp.NewServer(mcp.Info{Name: serverName, Version: serverVersion}, mcp.ServerDeps{
		Probe:   probe,
		Factory: factory,
		Storage: store,
		Clock:   clock,
		Logger:  logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := mcp.Serve(ctx, server); err != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", err)
		return 1
	}
	return 0
}
