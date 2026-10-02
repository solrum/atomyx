package iosdriver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// RunnerIdentity is the launch identity a Swift runner reports in its
// ping reply. A field is empty when the runner was started without
// the matching launch environment.
type RunnerIdentity struct {
	// RunnerID is the identifier the launcher assigned to this one
	// runner launch.
	RunnerID string
	// TargetUDID is the physical device the launcher aimed the runner
	// at. Xcode sets no equivalent for a device, so only a launcher
	// that passes it explicitly produces a value.
	TargetUDID string
	// SimulatorUDID is the simulator the runner executes in. Xcode
	// sets it for every simulator destination, whoever launched the
	// runner.
	SimulatorUDID string
}

// ExpectedRunner describes the runner a caller means to reach on a
// port, so an answer from a different runner is never adopted.
type ExpectedRunner struct {
	// UDID is the simulator or device the runner must belong to.
	UDID string
	// Kind selects which reported field proves the UDID.
	Kind Kind
	// RunnerID is the launch identifier the caller assigned. Empty
	// means the caller did not launch the runner itself, for example
	// one started by hand.
	RunnerID string
}

// IdentityVerdict classifies a runner's reported identity against an
// ExpectedRunner.
type IdentityVerdict int

const (
	// IdentityMatch means the runner proved it is the expected one.
	IdentityMatch IdentityVerdict = iota
	// IdentityUnmanaged means the runner proved it belongs to the
	// requested simulator but carries no launch identifier, so it was
	// not started by the caller. Accepted, and the caller warns.
	IdentityUnmanaged
	// IdentityMismatch means the runner is a different runner, or
	// reported nothing that proves otherwise.
	IdentityMismatch
	// IdentityLegacy means the runner answers but reports none of the
	// identity fields, so it was built before launch identities
	// existed. Refused like a mismatch, with a rebuild as the remedy.
	IdentityLegacy
)

// Verify compares got against the expected runner and returns the
// verdict with a plain-language reason for a mismatch.
//
// A caller-assigned RunnerID must be echoed back exactly. Without one,
// a simulator runner must report its own simulator UDID, and a device
// runner must report a matching targetUdid, because a device offers no
// other way to confirm identity. A reported UDID that disagrees with
// the expected one is a mismatch even when the runner id matches.
func (e ExpectedRunner) Verify(got RunnerIdentity) (IdentityVerdict, string) {
	if e.Kind == KindDevice {
		return e.verifyDevice(got)
	}
	return e.verifySimulator(got)
}

func (e ExpectedRunner) verifySimulator(got RunnerIdentity) (IdentityVerdict, string) {
	if got.SimulatorUDID != "" && !strings.EqualFold(got.SimulatorUDID, e.UDID) {
		return IdentityMismatch, fmt.Sprintf("the runner belongs to simulator %s, not %s", got.SimulatorUDID, e.UDID)
	}
	if e.RunnerID != "" {
		if got.RunnerID != e.RunnerID {
			return IdentityMismatch, fmt.Sprintf("the runner reports id %q, expected %q", got.RunnerID, e.RunnerID)
		}
		return IdentityMatch, ""
	}
	if got.SimulatorUDID == "" {
		return IdentityLegacy, "the runner reports no simulator identity, so it cannot be confirmed as simulator " + e.UDID
	}
	if got.RunnerID == "" {
		return IdentityUnmanaged, ""
	}
	return IdentityMatch, ""
}

func (e ExpectedRunner) verifyDevice(got RunnerIdentity) (IdentityVerdict, string) {
	if got.TargetUDID != "" && !strings.EqualFold(got.TargetUDID, e.UDID) {
		return IdentityMismatch, fmt.Sprintf("the runner targets device %s, not %s", got.TargetUDID, e.UDID)
	}
	if e.RunnerID != "" {
		if got.RunnerID != e.RunnerID {
			return IdentityMismatch, fmt.Sprintf("the runner reports id %q, expected %q", got.RunnerID, e.RunnerID)
		}
		return IdentityMatch, ""
	}
	if got.TargetUDID == "" {
		return IdentityLegacy, "the runner reports no target device, so it cannot be confirmed as device " + e.UDID
	}
	return IdentityMatch, ""
}

// ProbeRunnerIdentity sends the ping handshake to 127.0.0.1:port and
// returns the identity the answering runner reports. ok is false when
// nothing answers a valid ping, so a caller can tell "no runner" apart
// from "a different runner".
func ProbeRunnerIdentity(ctx context.Context, port int, timeout time.Duration) (id RunnerIdentity, ok bool) {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := new(net.Dialer).DialContext(dialCtx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return RunnerIdentity{}, false
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte(`{"id":0,"type":"ping","args":{}}` + "\n")); err != nil {
		return RunnerIdentity{}, false
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return RunnerIdentity{}, false
	}
	var frame struct {
		Ok   *bool `json:"ok"`
		Data struct {
			Pong          bool   `json:"pong"`
			RunnerID      string `json:"runnerId"`
			TargetUDID    string `json:"targetUdid"`
			SimulatorUDID string `json:"simulatorUdid"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		return RunnerIdentity{}, false
	}
	if frame.Ok == nil || !*frame.Ok || !frame.Data.Pong {
		return RunnerIdentity{}, false
	}
	return RunnerIdentity{
		RunnerID:      frame.Data.RunnerID,
		TargetUDID:    frame.Data.TargetUDID,
		SimulatorUDID: frame.Data.SimulatorUDID,
	}, true
}

// RunnerIdentityError reports a runner answering on a port that is
// not the expected one.
type RunnerIdentityError struct {
	Port   int
	Expect ExpectedRunner
	Reason string
	// Legacy is true when the runner reports no identity at all, so the
	// fix is to rebuild it instead of choosing another port.
	Legacy bool
}

func (e *RunnerIdentityError) Error() string {
	if e.Legacy {
		return fmt.Sprintf(
			"the runner on 127.0.0.1:%d predates launch identities: %s. Stop it, rebuild the runner "+
				"(cd platforms/ios-agent && make build), and start it again",
			e.Port, e.Reason,
		)
	}
	return fmt.Sprintf(
		"a runner on 127.0.0.1:%d answers for a different target: %s. Stop it or choose another port before connecting to %s",
		e.Port, e.Reason, e.Expect.UDID,
	)
}

// CheckRunnerIdentity probes port and verifies the answering runner is
// the expected one. found is false when nothing answers a ping, with a
// nil error, so a caller decides whether absence is acceptable. unmanaged is true when the
// runner was accepted without a launch identifier.
func CheckRunnerIdentity(ctx context.Context, port int, timeout time.Duration, want ExpectedRunner) (found, unmanaged bool, err *RunnerIdentityError) {
	got, ok := ProbeRunnerIdentity(ctx, port, timeout)
	if !ok {
		return false, false, nil
	}
	verdict, reason := want.Verify(got)
	if verdict == IdentityMismatch || verdict == IdentityLegacy {
		return true, false, &RunnerIdentityError{Port: port, Expect: want, Reason: reason, Legacy: verdict == IdentityLegacy}
	}
	return true, verdict == IdentityUnmanaged, nil
}

// RunnerProbeTimeout bounds one identity probe of a runner port.
const RunnerProbeTimeout = iproxyPingProbeTimeout
