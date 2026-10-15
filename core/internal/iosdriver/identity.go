package iosdriver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
	// ProtocolVersion is the wire contract version the runner declares.
	// It is zero for a runner that reports none.
	ProtocolVersion int
	// AgentVersion is the runner build version, for display only. It is
	// empty for a runner that reports none.
	AgentVersion string
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

// IsPreviousDeviceRunner reports whether got is an earlier runner of the
// expected physical device: it targets the same device but reports a
// different launch id than the one this launch assigned. A device
// exposes one fixed runner port, so such a runner is the predecessor
// still occupying that port, not a runner of another target.
func (e ExpectedRunner) IsPreviousDeviceRunner(got RunnerIdentity) bool {
	return e.Kind == KindDevice &&
		e.RunnerID != "" &&
		got.TargetUDID != "" &&
		strings.EqualFold(got.TargetUDID, e.UDID) &&
		got.RunnerID != e.RunnerID
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
	id, outcome := probeRunner(ctx, port, timeout)
	return id, outcome == probeAnswered
}

// probeOutcome classifies how a listener reacted to the ping handshake.
type probeOutcome int

const (
	// probeAnswered means the listener replied with a valid pong.
	probeAnswered probeOutcome = iota
	// probeSilent means the listener accepted the connection but gave no
	// reply before the deadline, which is how a runner that serves one
	// client at a time looks to a second client.
	probeSilent
	// probeRejected means the listener refused, closed the connection, or
	// replied with something that is not the runner protocol.
	probeRejected
)

func silentIfTimeout(err error) probeOutcome {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return probeSilent
	}
	return probeRejected
}

// probeRunner is ProbeRunnerIdentity with the reason a probe failed.
func probeRunner(ctx context.Context, port int, timeout time.Duration) (RunnerIdentity, probeOutcome) {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := new(net.Dialer).DialContext(dialCtx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return RunnerIdentity{}, silentIfTimeout(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte(`{"id":0,"type":"ping","args":{}}` + "\n")); err != nil {
		return RunnerIdentity{}, silentIfTimeout(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return RunnerIdentity{}, silentIfTimeout(err)
	}
	var frame struct {
		Ok   *bool `json:"ok"`
		Data struct {
			Pong            bool   `json:"pong"`
			RunnerID        string `json:"runnerId"`
			TargetUDID      string `json:"targetUdid"`
			SimulatorUDID   string `json:"simulatorUdid"`
			ProtocolVersion int    `json:"protocolVersion"`
			AgentVersion    string `json:"agentVersion"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		return RunnerIdentity{}, probeRejected
	}
	if frame.Ok == nil || !*frame.Ok || !frame.Data.Pong {
		return RunnerIdentity{}, probeRejected
	}
	return RunnerIdentity{
		RunnerID:        frame.Data.RunnerID,
		TargetUDID:      frame.Data.TargetUDID,
		SimulatorUDID:   frame.Data.SimulatorUDID,
		ProtocolVersion: frame.Data.ProtocolVersion,
		AgentVersion:    frame.Data.AgentVersion,
	}, probeAnswered
}

// RunnerIdentityError reports a runner answering on a port that is
// not the expected one.
type RunnerIdentityError struct {
	Port   int
	Expect ExpectedRunner
	// Got is the identity the answering runner reported.
	Got    RunnerIdentity
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
	_, found, unmanaged, err = CheckRunner(ctx, port, timeout, want)
	return found, unmanaged, err
}

// CheckRunner behaves like CheckRunnerIdentity and also returns the
// identity the answering runner reported, so a caller can read its
// protocol version without a second probe. got is the zero value when
// nothing answers.
func CheckRunner(ctx context.Context, port int, timeout time.Duration, want ExpectedRunner) (got RunnerIdentity, found, unmanaged bool, err *RunnerIdentityError) {
	got, ok := ProbeRunnerIdentity(ctx, port, timeout)
	if !ok {
		return RunnerIdentity{}, false, false, nil
	}
	found, unmanaged, err = judgeRunner(port, got, want)
	return got, found, unmanaged, err
}

// judgeRunner applies the identity rule to the identity an answering
// runner reported.
func judgeRunner(port int, got RunnerIdentity, want ExpectedRunner) (found, unmanaged bool, err *RunnerIdentityError) {
	verdict, reason := want.Verify(got)
	if verdict == IdentityMismatch || verdict == IdentityLegacy {
		return true, false, &RunnerIdentityError{Port: port, Expect: want, Got: got, Reason: reason, Legacy: verdict == IdentityLegacy}
	}
	return true, verdict == IdentityUnmanaged, nil
}

// RunnerProbeTimeout bounds one identity probe of a runner port.
const RunnerProbeTimeout = iproxyPingProbeTimeout
