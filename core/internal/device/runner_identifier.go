package device

import (
	"context"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

// ServedRunner is what an iOS runner already answering on a local port
// reports about the device it belongs to. A field is empty when the
// runner does not report it.
type ServedRunner struct {
	// SimulatorUDID is the simulator the runner executes in.
	SimulatorUDID string
	// TargetUDID is the physical device the runner was aimed at.
	TargetUDID string
	// ProtocolVersion is the wire protocol version the runner declares,
	// zero when it declares none.
	ProtocolVersion int
}

// RunnerIdentifier reports the identity of the iOS runner answering on a
// local port, so a caller can name the device an already-served runner
// belongs to. ok is false when nothing answers.
type RunnerIdentifier interface {
	IdentifyRunner(ctx context.Context, port int) (runner ServedRunner, ok bool)
}

// PingRunnerIdentifier identifies a runner by sending it the ping
// handshake on 127.0.0.1.
type PingRunnerIdentifier struct{}

var _ RunnerIdentifier = PingRunnerIdentifier{}

// IdentifyRunner implements RunnerIdentifier.
func (PingRunnerIdentifier) IdentifyRunner(ctx context.Context, port int) (ServedRunner, bool) {
	got, ok := iosdriver.ProbeRunnerIdentity(ctx, port, iosdriver.RunnerProbeTimeout)
	if !ok {
		return ServedRunner{}, false
	}
	return ServedRunner{SimulatorUDID: got.SimulatorUDID, TargetUDID: got.TargetUDID, ProtocolVersion: got.ProtocolVersion}, true
}

// DeviceRunnerIdentifier identifies the runner a physical device is
// serving by opening a USB tunnel to the device, because a physical
// device has no host listener until a tunnel exists. ok is false when no
// runner answers through the tunnel or the tunnel cannot be opened.
type DeviceRunnerIdentifier interface {
	IdentifyDeviceRunner(ctx context.Context, udid string) (runner ServedRunner, ok bool)
}

// TunnelRunnerIdentifier identifies a runner on the local control port
// with the ping handshake and, for a physical device, through a
// short-lived tunnel on a free host port that it stops before it
// returns. The tunnel never occupies the control port, so the driver
// that connects afterwards opens its own tunnel there.
type TunnelRunnerIdentifier struct {
	// Background starts the tunnel process. Nil disables the device
	// lookup.
	Background process.BackgroundRunner
	// Clock times the wait for the tunnel to come up. Nil means the
	// system clock.
	Clock clock.Clock

	// freePort asks the operating system for a free host port. Nil
	// means operatingSystemFreePort.
	freePort func() (int, error)
}

var (
	_ RunnerIdentifier       = TunnelRunnerIdentifier{}
	_ DeviceRunnerIdentifier = TunnelRunnerIdentifier{}
)

// IdentifyRunner implements RunnerIdentifier.
func (TunnelRunnerIdentifier) IdentifyRunner(ctx context.Context, port int) (ServedRunner, bool) {
	return PingRunnerIdentifier{}.IdentifyRunner(ctx, port)
}

// IdentifyDeviceRunner implements DeviceRunnerIdentifier.
func (t TunnelRunnerIdentifier) IdentifyDeviceRunner(ctx context.Context, udid string) (ServedRunner, bool) {
	if t.Background == nil {
		return ServedRunner{}, false
	}
	freePort := t.freePort
	if freePort == nil {
		freePort = operatingSystemFreePort
	}
	port, err := freePort()
	if err != nil {
		return ServedRunner{}, false
	}
	clk := t.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	stop, _, err := iosdriver.StartTunnel(ctx, t.Background, clk, iosdriver.TunnelConfig{UDID: udid, HostPort: port, FreshPort: true})
	if err != nil {
		return ServedRunner{}, false
	}
	if stop != nil {
		defer stop()
	}
	return PingRunnerIdentifier{}.IdentifyRunner(ctx, port)
}
