package device

import (
	"context"
	"os"

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
}

// Factory maps a Descriptor to a live Built pair.
type Factory interface {
	Build(ctx context.Context, desc Descriptor) (Built, error)
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
	return &DriverFactory{runner: deps.Runner, background: background, clock: clock, simHid: deps.SimHid, logger: logger}
}

var _ Factory = (*DriverFactory)(nil)

// Build implements Factory.
func (f *DriverFactory) Build(ctx context.Context, desc Descriptor) (Built, error) {
	drv, err := f.createDriver(ctx, desc)
	if err != nil {
		return Built{}, err
	}
	if err := drv.Connect(ctx); err != nil {
		// Connect can fail after it has already started a long-lived
		// child, such as the USB tunnel, and the caller never receives
		// the driver to disconnect. Disconnect is idempotent, so
		// releasing here is always safe.
		_ = drv.Disconnect()
		return Built{}, err
	}
	return Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: f.clock, Logger: f.logger}),
	}, nil
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
	if desc.Platform == PlatformAndroid {
		return androiddriver.New(nil, f.runner, androiddriver.Options{
			Serial:           desc.ID,
			AllocateHostPort: true,
			DevicePort:       androidControlPort,
			NewTransport: func(hostPort int) androiddriver.Transport {
				return transport.NewHTTPClient("127.0.0.1", hostPort, transport.WithHTTPLogger(f.logger))
			},
		}), nil
	}

	kind := iosdriver.KindDevice
	if desc.Kind == KindSimulator {
		kind = iosdriver.KindSimulator
	}
	port := iosControlPort
	if desc.Port != 0 {
		port = desc.Port
	}
	conn := transport.NewTCPClient("127.0.0.1", port, transport.WithTCPLogger(f.logger))
	iosOpts := iosdriver.Options{UDID: desc.ID, Kind: kind, Port: port, Background: f.background}

	hidPort := desc.HidPort
	if desc.Kind == KindSimulator && hidPort == 0 && f.simHid != nil &&
		os.Getenv(simHidOptInEnv) == "1" && iossimdriver.IsSimDirectSupported(ctx, f.runner) {
		if p, err := f.simHid.StartSimHid(ctx, desc.ID); err == nil {
			hidPort = p
		}
	}

	if desc.Kind == KindSimulator && hidPort != 0 {
		return iossimdriver.New(conn, f.runner, iossimdriver.NewDialer(), iossimdriver.Options{
			Options: iosOpts,
			HidPort: hidPort,
		})
	}
	return iosdriver.New(conn, f.runner, iosOpts), nil
}

// MockDriverFactory replaces DriverFactory in tests: it returns a
// mock.Driver wrapped in a real Orchestra, with no adb, iproxy, or
// network access.
type MockDriverFactory struct {
	clock orchestra.Clock
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

var _ Factory = (*MockDriverFactory)(nil)

// Build implements Factory. The Descriptor is accepted for interface
// parity with DriverFactory but otherwise ignored. The mock driver
// carries no platform-specific behavior.
func (f *MockDriverFactory) Build(ctx context.Context, _ Descriptor) (Built, error) {
	drv := mock.New()
	if err := drv.Connect(ctx); err != nil {
		return Built{}, err
	}
	return Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: f.clock}),
	}, nil
}
