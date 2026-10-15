package device

import (
	"context"
	"os"
	"time"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/iossimdriver"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// androidControlPort and iosControlPort are the fixed ports the
// Android and iOS agents listen on. The Android port is the
// device-side port only, since the host-side port is allocated per
// device. The iOS port is both dialed by the transport client and
// passed as the driver's own port option, so those two must agree.
const (
	androidControlPort = 8765
	iosControlPort     = 22087
)

// adoptedRunnerProbeTimeout bounds the hierarchy call that proves an
// adopted runner still serves commands. A runner that does not answer in
// this time is ended and a new one is launched.
const adoptedRunnerProbeTimeout = 10 * time.Second

// simHidOptInEnv gates whether an iOS simulator target takes the
// direct-HID path at all: "1" opts in, anything else (including
// unset) keeps the XCUITest path. It is distinct from
// ATOMYX_SIM_HID_BIN, the helper binary override
// iossimdriver.SimHidLauncher reads.
const simHidOptInEnv = "ATOMYX_SIM_HID"

// Built is the live driver.Driver plus orchestra.Orchestra pair a
// Factory produces for one device.
type Built struct {
	Driver    driver.Driver
	Orchestra *orchestra.Orchestra
	// Lost receives one *ProcessExitError when a process the driver's
	// control plane depends on exits on its own while the driver is
	// connected. A Factory whose platform has no such observable signal
	// leaves it nil, and a nil channel never delivers.
	Lost <-chan error
	// Release stops what the build launched for the driver alone, such as
	// the iOS runner of a provisioned target. Disconnecting the driver
	// never does, because a runner outlives the session that used it. A
	// caller that owns the driver outright and never hands the runner on,
	// such as the Studio sidecar, calls it after Disconnect. It is
	// nil from a factory that launches nothing, and safe to call more than
	// once.
	Release func()
}

// Factory maps a Descriptor to a live Built pair.
type Factory interface {
	Build(ctx context.Context, desc Descriptor) (Built, error)
}

// DriverWrap decorates a freshly created driver. It must forward every
// call to the driver it receives.
type DriverWrap func(driver.Driver) driver.Driver

// WrappingFactory is a Factory that applies a DriverWrap before it
// builds the Orchestra, so the Orchestra and Built.Driver both use the
// wrapped driver. WithDriverWrap returns a new Factory and leaves the
// receiver unchanged. Wraps accumulate, and the one added last is the
// outermost.
type WrappingFactory interface {
	Factory
	WithDriverWrap(wrap DriverWrap) Factory
}

func chainWrap(inner, outer DriverWrap) DriverWrap {
	switch {
	case outer == nil:
		return inner
	case inner == nil:
		return outer
	default:
		return func(d driver.Driver) driver.Driver { return outer(inner(d)) }
	}
}

func applyWrap(wrap DriverWrap, d driver.Driver) driver.Driver {
	if wrap == nil {
		return d
	}
	return wrap(d)
}

// DriverFactoryDeps configures a DriverFactory. A nil Clock defaults
// to orchestra.SystemClock{}. A nil SimHid disables the direct-HID
// path entirely regardless of ATOMYX_SIM_HID, matching a caller that
// never wires a SimHidManager. A nil Logger defaults to
// logging.NoopLogger{}, so a caller that never wires one keeps
// building drivers and an Orchestra that log nothing.
type DriverFactoryDeps struct {
	Runner process.Runner
	// Background starts the long-lived processes an iOS driver
	// supervises, such as the USB tunnel to a physical device. It is
	// required: NewDriverFactory panics when it is nil.
	Background process.BackgroundRunner
	Clock      orchestra.Clock
	// SimHid starts the atomyx-sim-hid helper for an iOS simulator
	// target that opts into the direct-HID path (see simHidOptInEnv).
	// *SimHidManager implements this.
	SimHid simHidStarter
	// Logger is wired into every built driver's underlying transport
	// client and into the Orchestra wrapping it, so a call made under a
	// context carrying a run id (see logging.WithRunID) logs a
	// completion line correlated to that run.
	Logger logging.Logger
	// Provisioner launches the XCUITest runner for an iOS target that
	// names no port of its own, so a build needs no runner started by
	// hand. The built driver owns the returned kill and runs it in
	// Disconnect. A nil Provisioner leaves the factory dialing the fixed
	// control port, which a caller that runs its own runner relies on.
	Provisioner AgentProvisioner
	// WrapDriver decorates every driver the factory creates, before it
	// connects and before the Orchestra is built over it. Nil leaves
	// the driver unwrapped.
	WrapDriver DriverWrap
	// OwnAndroidForwards makes an Android driver remove an adb forward
	// it reused once the agent answered through it. Only a composition
	// root whose sessions hold a device lease sets it, because a host
	// without the lease cannot know another process is done with the
	// forward.
	OwnAndroidForwards bool
}

// DriverFactory picks the concrete driver family for a device's
// platform, connects it, and wraps it in an Orchestra. It holds no
// per-device state. Each Build call produces a fresh pair. Swapping
// an entire driver family (say, a future WebDriver-based adapter)
// means editing this type; Probe and its callers never learn a new
// family exists.
type DriverFactory struct {
	runner     process.Runner
	background process.BackgroundRunner
	clock      orchestra.Clock
	simHid     simHidStarter
	logger     logging.Logger

	provisioner AgentProvisioner
	wrap        DriverWrap

	ownAndroidForwards bool

	// probeAdopted proves an adopted runner serves commands.
	probeAdopted func(ctx context.Context, d driver.Driver) error
}

// NewDriverFactory constructs a DriverFactory from deps.
func NewDriverFactory(deps DriverFactoryDeps) *DriverFactory {
	clock := deps.Clock
	if clock == nil {
		clock = orchestra.SystemClock{}
	}
	logger := deps.Logger
	if logger == nil {
		logger = logging.NoopLogger{}
	}
	background := deps.Background
	if background == nil {
		panic("device.NewDriverFactory: Background runner is required")
	}
	return &DriverFactory{
		runner: deps.Runner, background: background, clock: clock, simHid: deps.SimHid, logger: logger,
		provisioner: deps.Provisioner, wrap: deps.WrapDriver, ownAndroidForwards: deps.OwnAndroidForwards,
		probeAdopted: probeHierarchy,
	}
}

// probeHierarchy proves a connected driver serves commands with one
// hierarchy call bounded by adoptedRunnerProbeTimeout.
func probeHierarchy(ctx context.Context, d driver.Driver) error {
	probeCtx, cancel := context.WithTimeout(ctx, adoptedRunnerProbeTimeout)
	defer cancel()
	_, err := d.Hierarchy(probeCtx)
	return err
}

var _ WrappingFactory = (*DriverFactory)(nil)

// WithDriverWrap implements WrappingFactory.
func (f *DriverFactory) WithDriverWrap(wrap DriverWrap) Factory {
	wrapped := *f
	wrapped.wrap = chainWrap(f.wrap, wrap)
	return &wrapped
}

// Build implements Factory. A runner the provisioner adopted must
// connect and then answer one hierarchy call. When it does not, it is
// ended and the build repeats once with a runner launched fresh.
func (f *DriverFactory) Build(ctx context.Context, desc Descriptor) (Built, error) {
	lost := make(chan error, 1)
	onLost := func(cause error) {
		select {
		case lost <- cause:
		default:
		}
	}
	opts := ProvisionOptions{Lender: LenderFrom(ctx)}
	retried := false
	for {
		created, err := f.createDriverWatched(ctx, desc, onLost, opts)
		if err != nil {
			return Built{}, err
		}
		drv := applyWrap(f.wrap, created.driver)
		connectErr := drv.Connect(ctx)
		if connectErr == nil && created.adopted {
			connectErr = f.probeAdopted(ctx, created.driver)
		}
		if connectErr == nil {
			return Built{
				Driver:    drv,
				Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: f.clock, Logger: f.logger}),
				Lost:      lost,
				Release:   created.release,
			}, nil
		}
		// Connect can fail after it has already started a long-lived
		// child, such as the USB tunnel, and the caller never receives
		// the driver to disconnect. Disconnect is idempotent, so
		// releasing here is always safe. The runner was never used, so
		// it stops with the driver.
		_ = drv.Disconnect()
		created.release()
		if !created.adopted || retried || ctx.Err() != nil {
			return Built{}, connectErr
		}
		f.logger.Warn("the adopted runner did not answer, launching a new one", logging.LogContext{"udid": desc.ID, "error": connectErr.Error()})
		opts.Fresh = true
		retried = true
	}
}

// createDriver picks the concrete driver for desc. An iOS simulator
// descriptor that already carries a non-zero HidPort, set by a caller
// that started the atomyx-sim-hid helper itself, always builds an
// iossimdriver.IosSimDriver. Otherwise, for an iOS simulator target,
// createDriver takes the direct-HID path itself when the host opts in
// with simHidOptInEnv="1" and a SimHidManager was injected and
// iossimdriver.IsSimDirectSupported reports the host meets the
// capability floor: it starts the helper through f.simHid and builds
// an IosSimDriver around the port it returns. A StartSimHid failure
// (the helper binary missing, an invalid handshake, an early exit)
// falls back to the XCUITest path below instead of failing the build.
// A device target, or a simulator target that never qualifies for the
// direct-HID path, always gets the XCUITest path.
func (f *DriverFactory) createDriver(ctx context.Context, desc Descriptor) (driver.Driver, error) {
	created, err := f.createDriverWatched(ctx, desc, nil, ProvisionOptions{})
	return created.driver, err
}

// createdDriver is a driver createDriverWatched built, not yet connected.
type createdDriver struct {
	driver driver.Driver
	// release stops the runner the driver was provisioned with. It is
	// never nil on success.
	release func()
	// adopted reports that the runner was already running and has not
	// yet answered a command through this driver.
	adopted bool
}

// createDriverWatched is createDriver with onLost wired to the loss
// signal of a provisioned iOS runner, and opts passed to a provisioner
// that can adopt a runner. A platform without an observable signal
// ignores onLost.
func (f *DriverFactory) createDriverWatched(ctx context.Context, desc Descriptor, onLost func(error), opts ProvisionOptions) (createdDriver, error) {
	if desc.Platform == PlatformAndroid {
		androidOpts := androiddriver.Options{
			Serial:              desc.ID,
			AllocateHostPort:    true,
			DevicePort:          androidControlPort,
			OwnAnsweringForward: f.ownAndroidForwards,
			NewTransport: func(hostPort int) androiddriver.Transport {
				return transport.NewHTTPClient("127.0.0.1", hostPort, transport.WithHTTPLogger(f.logger))
			},
		}
		if f.provisioner != nil {
			androidOpts.ReinstallAgent = func(ctx context.Context) error {
				return f.provisioner.ReinstallAndroidAgent(ctx, desc.ID)
			}
		}
		return createdDriver{driver: androiddriver.New(nil, f.runner, androidOpts), release: func() {}}, nil
	}

	kind := iosdriver.KindDevice
	agentKind := IosAgentKindDevice
	if desc.Kind == KindSimulator {
		kind = iosdriver.KindSimulator
		agentKind = IosAgentKindSimulator
	}
	port := iosControlPort
	if desc.Port != 0 {
		port = desc.Port
	}
	iosOpts := iosdriver.Options{
		UDID: desc.ID, Kind: kind, Port: port, Background: f.background,
		OwnRunnerBundleID: iosdriver.DefaultRunnerBundleID,
		OwnRunnerAppName:  iosdriver.RunnerAppName,
	}

	// A descriptor that names its own port belongs to a caller that runs
	// its own runner there, and a descriptor with no id names no target
	// to launch a runner for, so only a descriptor with an id and
	// without a port is provisioned.
	stopRunner := func() {}
	adopted := false
	if desc.Port != 0 && desc.ID != "" {
		f.logger.Info("using the runner served on the control port, and Atomyx will not stop it", logging.LogContext{"udid": desc.ID, "port": desc.Port})
	}
	if f.provisioner != nil && desc.Port == 0 && desc.ID != "" {
		agent, err := f.provisioner.ProvisionIOSAgent(ctx, desc.ID, agentKind, onLost, opts)
		if err != nil {
			if agent.Kill != nil {
				_ = agent.Kill()
			}
			return createdDriver{}, iosdriver.ProvisionFailure(desc.ID, err)
		}
		port = agent.Port
		adopted = agent.Adopted
		stopRunner = func() { _ = agent.Kill() }
		iosOpts.Port = port
		iosOpts.RunnerID = agent.RunnerID
		iosOpts.OwnRunnerBundleID = iosdriver.ResolveRunnerBundleID(ctx, f.runner, agent.BuildProductPath, iosdriver.DefaultRunnerBundleID)
		iosOpts.ExternalTunnel = kind == iosdriver.KindDevice
	}
	conn := transport.NewTCPClient("127.0.0.1", port, transport.WithTCPLogger(f.logger))

	hidPort := desc.HidPort
	if desc.Kind == KindSimulator && hidPort == 0 && f.simHid != nil &&
		os.Getenv(simHidOptInEnv) == "1" && iossimdriver.IsSimDirectSupported(ctx, f.runner) {
		if p, err := f.simHid.StartSimHid(ctx, desc.ID); err == nil {
			hidPort = p
		}
	}

	if desc.Kind == KindSimulator && hidPort != 0 {
		if idErr := f.verifySimulatorRunner(ctx, port, desc.ID); idErr != nil {
			stopRunner()
			return createdDriver{}, idErr
		}
		drv, err := iossimdriver.New(conn, f.runner, iossimdriver.NewDialer(), iossimdriver.Options{
			Options: iosOpts,
			HidPort: hidPort,
		})
		if err != nil {
			stopRunner()
			return createdDriver{}, err
		}
		return createdDriver{driver: drv, release: stopRunner, adopted: adopted}, nil
	}
	return createdDriver{driver: iosdriver.New(conn, f.runner, iosOpts), release: stopRunner, adopted: adopted}, nil
}

// MockDriverFactory replaces DriverFactory in tests: it returns a
// mock.Driver wrapped in a real Orchestra, with no adb, iproxy, or
// network access.
type MockDriverFactory struct {
	clock orchestra.Clock
	wrap  DriverWrap
}

// NewMockDriverFactory constructs a MockDriverFactory whose Orchestra
// runs on the real-time SystemClock.
func NewMockDriverFactory() *MockDriverFactory {
	return &MockDriverFactory{clock: orchestra.SystemClock{}}
}

// MockDriverFactoryDeps configures a MockDriverFactory. A nil Clock
// defaults to orchestra.SystemClock{}.
type MockDriverFactoryDeps struct {
	Clock orchestra.Clock
}

// NewMockDriverFactoryWithDeps constructs a MockDriverFactory whose
// Orchestra runs on deps.Clock instead of the real-time SystemClock, since
// a device-free differential harness uses an orchestra.FakeClock here
// so every internal Finder and ScrollController poll loop is
// deterministic.
func NewMockDriverFactoryWithDeps(deps MockDriverFactoryDeps) *MockDriverFactory {
	clock := deps.Clock
	if clock == nil {
		clock = orchestra.SystemClock{}
	}
	return &MockDriverFactory{clock: clock}
}

var _ WrappingFactory = (*MockDriverFactory)(nil)

// WithDriverWrap implements WrappingFactory.
func (f *MockDriverFactory) WithDriverWrap(wrap DriverWrap) Factory {
	wrapped := *f
	wrapped.wrap = chainWrap(f.wrap, wrap)
	return &wrapped
}

// Build implements Factory. The Descriptor is accepted for interface
// parity with DriverFactory but otherwise ignored. The mock driver
// carries no platform-specific behavior.
func (f *MockDriverFactory) Build(ctx context.Context, _ Descriptor) (Built, error) {
	drv := applyWrap(f.wrap, mock.New())
	if err := drv.Connect(ctx); err != nil {
		return Built{}, err
	}
	return Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: f.clock}),
	}, nil
}

// verifySimulatorRunner confirms that a runner already answering on
// port belongs to the simulator udid, so a runner left behind by a
// different simulator is never adopted by a direct-HID driver. Nothing
// answering is not an error, because the driver's own connect reports
// an unreachable runner. A runner that answers without proving it
// belongs to udid is refused.
func (f *DriverFactory) verifySimulatorRunner(ctx context.Context, port int, udid string) error {
	want := iosdriver.ExpectedRunner{UDID: udid, Kind: iosdriver.KindSimulator}
	_, unmanaged, idErr := iosdriver.CheckRunnerIdentity(ctx, port, iosdriver.RunnerProbeTimeout, want)
	if idErr != nil {
		return idErr
	}
	if unmanaged {
		f.logger.Warn("adopting a simulator runner that carries no launch id", logging.LogContext{"port": port, "udid": udid})
	}
	return nil
}
