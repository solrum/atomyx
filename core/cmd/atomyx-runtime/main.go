// Command atomyx-runtime is the sidecar process: a newline-delimited
// JSON-RPC bridge over stdin/stdout that drives a real Android or iOS
// device on the host's behalf. A parent process (the CLI, Studio)
// spawns it, sends one JSON request per line, and reads one JSON
// response or event per line back.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/shutdown"
	"dev.atomyx/internal/sidecar"
	"dev.atomyx/internal/version"
)

// binaryName identifies this binary on its own --version line.
const binaryName = "atomyx-runtime"

func main() {
	if versionRequested(os.Args[1:]) {
		fmt.Println(versionLine())
		os.Exit(0)
	}
	os.Exit(run())
}

// versionRequested reports whether args asked for this binary's build
// identity instead of starting the sidecar. atomyx-runtime has no
// other flags or subcommands, so a bare --version or -v anywhere main
// forwards it is enough.
func versionRequested(args []string) bool {
	return len(args) > 0 && (args[0] == "--version" || args[0] == "-v")
}

// versionLine is the single line atomyx-runtime reports for
// --version: its own binary name plus the build identity every
// Atomyx binary shares.
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
	simHid := device.NewSimHidManager(background)
	factory := device.NewDriverFactory(device.DriverFactoryDeps{Runner: procRunner, Background: background, Clock: clock, SimHid: simHid, Logger: logger})
	provisioner := device.NewDriverProvisioner(device.DriverProvisionerDeps{Runner: procRunner, Background: background, Clock: clock, FixedIosPort: true})

	dispatcher := sidecar.NewDispatcher()
	events := sidecar.NewEventBus()
	session := sidecar.NewSession()
	ctx := &sidecar.Context{Dispatcher: dispatcher, Events: events, Session: session}

	// Agent readiness services are constructed here, ahead of the
	// registration array, so the same instances wire into both their
	// own provisioning feature and RegisterMirrorInputFeature, which
	// only reads readiness.
	iosAgentService := sidecar.NewIosAgentService(provisioner, events, onAgentPanic)
	androidAgentService := sidecar.NewAndroidAgentService(provisioner, events, onAgentPanic)

	handles := []sidecar.Handle{
		sidecar.RegisterMetaFeature(ctx),
		sidecar.RegisterDeviceFeature(ctx, probe, factory),
		sidecar.RegisterAppFeature(ctx),
		sidecar.RegisterScriptFeature(ctx, logger),
		sidecar.RegisterInspectionFeature(ctx),
		sidecar.RegisterInputFeature(ctx),
		sidecar.RegisterIosAgentFeature(ctx, iosAgentService, simHid),
		sidecar.RegisterAndroidAgentFeature(ctx, androidAgentService),
		sidecar.RegisterMirrorInputFeature(ctx, sidecar.MirrorInputFeatureDeps{
			IosAgent:     iosAgentService,
			AndroidAgent: androidAgentService,
		}),
	}

	transport := sidecar.NewTransport(os.Stdin, os.Stdout, dispatcher, onTransportError)
	unsubscribe := events.Subscribe(func(event sidecar.Event) { transport.Emit(event) })
	defer unsubscribe()

	disposeOnce := sync.OnceFunc(func() { dispose(handles, session, simHid) })

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// Run's return value is always either nil (stopped by EOF or a
		// read error, not by runCtx) or runCtx's own cancellation error,
		// which the two select cases below already know the reason for:
		// it carries nothing the shutdown path here still needs.
		_ = transport.Run(runCtx)
		close(done)
	}()

	select {
	case sig := <-sigCh:
		// Run's ReadString blocks on stdin regardless of ctx, so
		// cancellation alone never unblocks it: closing stdin makes
		// the pending read return, which lets Run observe ctx.Err()
		// and drain every in-flight dispatch before done closes. The
		// watchdog bounds the drain and the dispose together to one
		// grace period, disposing before it forces the exit.
		shutdown.HandleStop(clock, sig, shutdown.StopHooks{
			Cancel:     cancel,
			CloseInput: os.Stdin.Close,
			Cleanup:    disposeOnce,
			Exit:       os.Exit,
		})
		<-done
	case <-done:
		cancel()
	}
	if !shutdown.Bounded(runCtx, clock, shutdown.GracePeriod, disposeOnce) {
		writeSidecarErrorLine("device cleanup did not finish in time")
	}
	return 0
}

// dispose releases every feature's resources, the sim-HID manager's
// (killing any running atomyx-sim-hid helper), and finally the
// session's own (the currently selected device, if any). A feature's
// Dispose error is discarded: a teardown failure must not prevent the
// rest of shutdown from proceeding.
func dispose(handles []sidecar.Handle, session *sidecar.Session, simHid *device.SimHidManager) {
	for _, h := range handles {
		if h.Dispose == nil {
			continue
		}
		_ = h.Dispose()
	}
	simHid.Dispose()
	session.Dispose()
}

type sidecarErrorPayload struct {
	Message string `json:"message"`
}

type sidecarErrorEvent struct {
	Event   string              `json:"event"`
	Payload sidecarErrorPayload `json:"payload"`
}

// onTransportError reports a transport-level failure (a malformed
// input line, a response that failed to encode, a write failure) on
// stderr as one JSON line, out of band from the stdout request and
// event stream a host consumes.
func onTransportError(err error) {
	writeSidecarErrorLine(err.Error())
}

// onAgentPanic reports a recovered panic from a background device
// provisioning goroutine on stderr, the same out-of-band channel
// onTransportError uses, so a crash that would otherwise end the
// goroutine silently is still visible to the host.
func onAgentPanic(source string, recovered any, stack []byte) {
	writeSidecarErrorLine(fmt.Sprintf("%s panicked: %v\n%s", source, recovered, stack))
}

func writeSidecarErrorLine(message string) {
	encoded, marshalErr := json.Marshal(sidecarErrorEvent{
		Event:   "sidecarError",
		Payload: sidecarErrorPayload{Message: message},
	})
	if marshalErr != nil {
		return
	}
	encoded = append(encoded, '\n')
	_, _ = os.Stderr.Write(encoded)
}
