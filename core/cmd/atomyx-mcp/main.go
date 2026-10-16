// Command atomyx-mcp is the Atomyx MCP server: a stdio-based process
// an AI agent connects to for tool-driven mobile test orchestration
// over the 28-tool surface in dev.atomyx/internal/mcp.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dev.atomyx/cmd/internal/runnerprovision"
	"dev.atomyx/cmd/internal/sessionwire"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/mcp"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/runfolder"
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
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", err)
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if opts.version {
		fmt.Println(versionLine())
		os.Exit(0)
	}
	os.Exit(run(opts))
}

const usage = "usage: atomyx-mcp [--version] [--platform <ios|android> --device <id>]"

// options is what the command line asked for.
type options struct {
	version bool
	// pinned is the device the server is bound to, nil when the server
	// lets the agent choose.
	pinned *mcp.SelectDeviceInput
}

// parseArgs reads the command line. --device binds the server to one
// device and needs --platform, because a device id alone does not say
// which platform's discovery resolves it: select_device takes the same
// pair. A bare --version or -v asks for the build identity instead of
// starting the server.
func parseArgs(args []string) (options, error) {
	var opts options
	var platform, id string
	var sawDevice bool
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		switch name {
		case "--version", "-v":
			opts.version = true
		case "--device", "--platform":
			if !hasValue {
				i++
				if i >= len(args) {
					return options{}, fmt.Errorf("%s needs a value", name)
				}
				value = args[i]
			}
			if name == "--device" {
				id, sawDevice = value, true
			} else {
				platform = value
			}
		default:
			return options{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	if opts.version {
		return opts, nil
	}
	if !sawDevice && platform == "" {
		return opts, nil
	}
	if !sawDevice || id == "" {
		return options{}, errors.New("--platform needs --device")
	}
	switch device.Platform(platform) {
	case device.PlatformIOS, device.PlatformAndroid:
	default:
		return options{}, errors.New("--device needs --platform ios or --platform android")
	}
	opts.pinned = &mcp.SelectDeviceInput{Platform: device.Platform(platform), ID: id}
	return opts, nil
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
func run(opts options) int {
	procRunner := process.NewExec()
	background := process.NewExecBackground()
	clock := orchestra.SystemClock{}
	logger := logging.FromEnv()
	probe := device.NewCompositeProbe(
		logger,
		device.NewAndroidAdbProbe(procRunner),
		device.NewIosProbe(procRunner),
	)
	provisioner, keeper := runnerprovision.New(procRunner, background, clock, logger)
	factory := device.NewDriverFactory(device.DriverFactoryDeps{Runner: procRunner, Background: background, Clock: clock, Logger: logger, Provisioner: provisioner, OwnAndroidForwards: true})
	recorder := device.NewScreenRecorder(device.RecorderDeps{Runner: procRunner, Background: background, Logger: logger})

	// storage.NewFileStorage resolves its root as opts.Root, then the
	// ATOMYX_STORAGE_DIR environment variable, then "~/.atomyx". An
	// empty Root here defers entirely to that precedence.
	store, err := storage.NewFileStorage(storage.FileStorageOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", err)
		return 1
	}

	// Run folders live under the directory the store resolved. The
	// server derives its run catalog and its loose screenshots directory
	// from the starter, so the root is stated once.
	runStarter := runfolder.NewStarter(store.Root(), time.Now, rand.Reader)

	sessionDeps, err := sessionwire.Deps(procRunner, clock, logger, binaryName, "mcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", err)
		return 1
	}
	sessionDeps.Preparer = provisioner

	projectConfig := sessionwire.LoadConfigBestEffort(logger)

	server, err := mcp.NewServer(mcp.Info{Name: serverName, Version: serverVersion}, mcp.ServerDeps{
		Probe:          probe,
		Runners:        device.TunnelRunnerIdentifier{Background: background, Clock: clock},
		Factory:        factory,
		Storage:        store,
		RunStarter:     runStarter,
		Clock:          clock,
		Logger:         logger,
		Session:        sessionDeps,
		Pinned:         opts.pinned,
		Recorder:       recorder,
		VideoOnFailure: projectConfig.VideoOnFailure(),
		Timeouts:       sessionwire.ResolveTimeouts(projectConfig, os.Getenv, logger),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", err)
		return 1
	}

	cleanup := runnerprovision.Cleanup(keeper, server.Close)
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
			Cleanup:    cleanup,
			Exit:       os.Exit,
		})
	}()

	// The prune runs off the serving path so the handshake never waits
	// on it, and it applies only the policy the project config declares.
	go pruneRetention(projectConfig, store.Root(), time.Now(), logger, storage.PruneRoot)

	serveErr := mcp.Serve(ctx, server)

	// Serve returns on a stop signal and on the client closing stdin
	// alike. Either way the active device must be released here, because
	// its helper processes run in their own process group and a terminal
	// Ctrl-C does not reach them.
	if !shutdown.Bounded(ctx, clock, shutdown.GracePeriod, cleanup) {
		fmt.Fprintln(os.Stderr, "atomyx-mcp: device cleanup did not finish in time")
	}
	if serveErr != nil {
		fmt.Fprintf(os.Stderr, "atomyx-mcp: %v\n", serveErr)
		return 1
	}
	return 0
}
