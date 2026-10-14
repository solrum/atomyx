package iosdriver_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/version"
)

const otherProtocolVersion = version.AgentProtocolVersion + 6

func TestProbeRunnerIdentity_ReadsProtocolAndAgentVersion(t *testing.T) {
	port, cleanup := startRawRunnerListener(t, `"protocolVersion":`+strconv.Itoa(otherProtocolVersion)+`,"agentVersion":"2.4"`)
	defer cleanup()

	got, ok := iosdriver.ProbeRunnerIdentity(context.Background(), port, time.Second)
	if !ok {
		t.Fatal("ProbeRunnerIdentity() ok = false, want true")
	}
	if got.ProtocolVersion != otherProtocolVersion || got.AgentVersion != "2.4" {
		t.Fatalf("ProbeRunnerIdentity() = %+v, want protocol version %d and agent version 2.4", got, otherProtocolVersion)
	}
}

func TestProbeRunnerIdentity_MissingProtocolVersionIsZero(t *testing.T) {
	port, cleanup := startRawRunnerListener(t, `"runnerId":"r-1"`)
	defer cleanup()

	got, ok := iosdriver.ProbeRunnerIdentity(context.Background(), port, time.Second)
	if !ok {
		t.Fatal("ProbeRunnerIdentity() ok = false, want true")
	}
	if got.ProtocolVersion != 0 || got.AgentVersion != "" {
		t.Fatalf("ProbeRunnerIdentity() = %+v, want zero versions for a runner that reports none", got)
	}
	if got.SpeaksHostProtocol() {
		t.Fatal("SpeaksHostProtocol() = true for a runner that reports no version, want false")
	}
}

func TestRunnerIdentity_SpeaksHostProtocolOnlyOnExactMatch(t *testing.T) {
	tests := []struct {
		version int
		want    bool
	}{
		{version.AgentProtocolVersion, true},
		{version.AgentProtocolVersion + 1, false},
		{0, false},
	}
	for _, tt := range tests {
		if got := (iosdriver.RunnerIdentity{ProtocolVersion: tt.version}).SpeaksHostProtocol(); got != tt.want {
			t.Errorf("SpeaksHostProtocol() with version %d = %v, want %v", tt.version, got, tt.want)
		}
	}
}

// connectionsAccepted reports whether a new connection to port is still
// accepted, which shows the listener behind it was left running.
func connectionsAccepted(t *testing.T, port int) bool {
	t.Helper()
	dialer := net.Dialer{Timeout: time.Second}
	c, err := dialer.DialContext(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func TestXctestLauncher_EnsureRunning_FixedPortRunnerWithOtherProtocol_FailsAndLeavesItRunning(t *testing.T) {
	for name, fields := range map[string]string{
		"different version": `"simulatorUdid":"SIM-1","protocolVersion":` + strconv.Itoa(otherProtocolVersion),
		"no version":        `"simulatorUdid":"SIM-1"`,
	} {
		t.Run(name, func(t *testing.T) {
			port, cleanup := startRawRunnerListener(t, fields)
			defer cleanup()
			runner := &fakeXcodebuildRunner{}
			l := newSimLauncher(t, runner, iosdriver.LaunchConfig{Port: port})

			kill, err := l.EnsureRunning(context.Background())

			code, _, remedy := diag.Describe(err)
			if code != diag.CodeAgentVersionMismatch {
				t.Fatalf("EnsureRunning() error = %v, want code %s", err, diag.CodeAgentVersionMismatch)
			}
			if !strings.Contains(remedy, "Stop the runner") || !strings.Contains(remedy, "make build") || !strings.Contains(remedy, strconv.Itoa(port)) {
				t.Fatalf("remedy = %q, want it to name the port, the stop, and the rebuild command", remedy)
			}
			if kill == nil {
				t.Fatal("kill = nil, want a no-op")
			}
			if killErr := kill(); killErr != nil {
				t.Fatalf("kill() = %v, want nil", killErr)
			}
			if len(runner.Invocations()) != 0 {
				t.Fatalf("invocations = %+v, want none: the host must not build or spawn over a runner it does not own", runner.Invocations())
			}
			if !connectionsAccepted(t, port) {
				t.Fatal("the runner no longer accepts connections, want it left running")
			}
		})
	}
}

func TestIosDriver_Connect_RecordsAgentInfo(t *testing.T) {
	conn := newFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": version.AgentProtocolVersion, "agentVersion": "1.7", "capabilities": map[string]any{}}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	want := driver.AgentInfo{ProtocolVersion: version.AgentProtocolVersion, AgentVersion: "1.7"}
	if got := d.AgentInfo(); got != want {
		t.Fatalf("AgentInfo() = %+v, want %+v", got, want)
	}
}

func TestIosDriver_Connect_ProtocolMismatch_FailsWithoutTouchingTheRunner(t *testing.T) {
	for name, reply := range map[string]map[string]any{
		"different version": {"protocolVersion": otherProtocolVersion, "agentVersion": "9.9", "capabilities": map[string]any{}},
		"no version":        {"capabilities": map[string]any{}},
	} {
		t.Run(name, func(t *testing.T) {
			conn := newFakeConn()
			conn.Respond("ping", func(map[string]any) (map[string]any, error) { return reply, nil })
			runner := process.NewFake()
			d := iosdriver.New(conn, runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

			err := d.Connect(context.Background())

			code, message, remedy := diag.Describe(err)
			if code != diag.CodeAgentVersionMismatch {
				t.Fatalf("Connect() error = %v, want code %s", err, diag.CodeAgentVersionMismatch)
			}
			if message == "" || !strings.Contains(remedy, "make build") {
				t.Fatalf("message = %q, remedy = %q, want a message and a rebuild remedy", message, remedy)
			}
			var launchErr *iosdriver.XctestLauncherError
			if errors.As(err, &launchErr) {
				t.Fatalf("Connect() error = %v, want no launcher involvement", err)
			}
			if got := d.AgentInfo(); got != (driver.AgentInfo{}) {
				t.Fatalf("AgentInfo() = %+v after a failed Connect, want the zero value", got)
			}
			if len(runner.Invocations()) != 0 {
				t.Fatalf("process invocations = %+v, want none: Connect must not stop or relaunch a runner", runner.Invocations())
			}
		})
	}
}

func TestIosDriver_Connect_ProtocolMismatch_ClosesTheConnectionAndStopsTheTunnel(t *testing.T) {
	port := freeTCPPort(t)
	stopped := make(chan struct{})
	conn := newFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": otherProtocolVersion, "capabilities": map[string]any{}}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: &tunnelBackground{stopped: stopped},
	})

	err := d.Connect(context.Background())

	if code, _, _ := diag.Describe(err); code != diag.CodeAgentVersionMismatch {
		t.Fatalf("Connect() error = %v, want code %s", err, diag.CodeAgentVersionMismatch)
	}
	if d.IsConnected() {
		t.Fatal("IsConnected() = true after a protocol mismatch, want the connection closed")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("the iproxy tunnel is still running after a protocol mismatch")
	}
}
