package device

import (
	"context"

	"dev.atomyx/internal/iosdriver"
)

// ServedRunner is what an iOS runner already answering on a local port
// reports about the device it belongs to. A field is empty when the
// runner does not report it.
type ServedRunner struct {
	// SimulatorUDID is the simulator the runner executes in.
	SimulatorUDID string
	// TargetUDID is the physical device the runner was aimed at.
	TargetUDID string
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
	return ServedRunner{SimulatorUDID: got.SimulatorUDID, TargetUDID: got.TargetUDID}, true
}
