package iosdriver_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/iosdriver"
)

const previousRunnerTimeoutMs = 5_000

// startScriptedRunnerListener answers every ping with the identity
// fields identityFor returns for the zero-based index of that ping.
func startScriptedRunnerListener(t *testing.T, identityFor func(ping int) string) int {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var pings atomic.Int64
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
					return
				}
				index := int(pings.Add(1)) - 1
				_, _ = fmt.Fprint(c, pongReply(0, identityFor(index)))
			}(c)
		}
	}()
	t.Cleanup(func() { _ = l.Close() })
	return l.Addr().(*net.TCPAddr).Port
}

func newDeviceLauncherOnPort(t *testing.T, runner *fakeXcodebuildRunner, port int, allocations *atomic.Int64) *iosdriver.XctestLauncher {
	t.Helper()
	return newSimLauncher(t, runner, iosdriver.LaunchConfig{
		Kind:            iosdriver.KindDevice,
		UDID:            "DEV-1",
		DevTeam:         "TEAM",
		RunnerID:        "new",
		LaunchTimeoutMs: previousRunnerTimeoutMs,
		AllocatePort: func() (int, error) {
			allocations.Add(1)
			return port, nil
		},
	})
}

func TestXctestLauncher_EnsureRunning_Device_WaitsOutAPreviousRunnerOfTheSameDevice(t *testing.T) {
	const oldRunnerPings = 3
	port := startScriptedRunnerListener(t, func(ping int) string {
		if ping < oldRunnerPings {
			return `"runnerId":"old","targetUdid":"DEV-1"`
		}
		return `"runnerId":"new","targetUdid":"DEV-1"`
	})
	runner := &fakeXcodebuildRunner{}
	l := newDeviceLauncherOnPort(t, runner, port, new(atomic.Int64))

	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v, want success once the new runner answers", err)
	}
	defer func() { _ = kill() }()
	if got := len(runner.SpawnEnvs()); got != 1 {
		t.Fatalf("spawns = %d, want 1 because the previous runner is not a port conflict", got)
	}
}

func TestXctestLauncher_EnsureRunning_Device_PreviousRunnerThatNeverYieldsTimesOutWithItsCause(t *testing.T) {
	var allocations atomic.Int64
	port := startScriptedRunnerListener(t, func(int) string {
		return `"runnerId":"old","targetUdid":"DEV-1"`
	})
	runner := &fakeXcodebuildRunner{}
	l := newDeviceLauncherOnPort(t, runner, port, &allocations)

	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrLaunchTimeout)
	if !strings.Contains(err.Error(), "previous runner") {
		t.Fatalf("error = %q, want it to name the previous runner as the cause", err)
	}
	if got := len(runner.SpawnEnvs()); got != 1 {
		t.Fatalf("spawns = %d, want 1 because a stale runner is not retried on another port", got)
	}
	if got := allocations.Load(); got != 1 {
		t.Fatalf("port allocations = %d, want 1", got)
	}
}

func TestXctestLauncher_EnsureRunning_Device_RunnerOfAnotherDeviceIsStillAMismatch(t *testing.T) {
	port := startScriptedRunnerListener(t, func(int) string {
		return `"runnerId":"old","targetUdid":"DEV-2"`
	})
	runner := &fakeXcodebuildRunner{}
	l := newDeviceLauncherOnPort(t, runner, port, new(atomic.Int64))

	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrRunnerMismatch)
}

func TestXctestLauncher_EnsureRunning_Simulator_RunnerWithAnotherIDIsStillAMismatch(t *testing.T) {
	port := startScriptedRunnerListener(t, func(int) string {
		return `"runnerId":"old","simulatorUdid":"SIM-1"`
	})
	runner := &fakeXcodebuildRunner{}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{
		RunnerID:        "new",
		LaunchTimeoutMs: previousRunnerTimeoutMs,
		AllocatePort:    func() (int, error) { return port, nil },
	})

	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrRunnerMismatch)
}

func TestExpectedRunner_IsPreviousDeviceRunner(t *testing.T) {
	device := iosdriver.ExpectedRunner{UDID: "DEV-1", Kind: iosdriver.KindDevice, RunnerID: "new"}
	cases := []struct {
		name string
		want iosdriver.ExpectedRunner
		got  iosdriver.RunnerIdentity
		is   bool
	}{
		{"same device other id", device, iosdriver.RunnerIdentity{RunnerID: "old", TargetUDID: "dev-1"}, true},
		{"same device no id", device, iosdriver.RunnerIdentity{TargetUDID: "DEV-1"}, true},
		{"same device same id", device, iosdriver.RunnerIdentity{RunnerID: "new", TargetUDID: "DEV-1"}, false},
		{"other device", device, iosdriver.RunnerIdentity{RunnerID: "old", TargetUDID: "DEV-2"}, false},
		{"no target", device, iosdriver.RunnerIdentity{RunnerID: "old"}, false},
		{"no expected id", iosdriver.ExpectedRunner{UDID: "DEV-1", Kind: iosdriver.KindDevice}, iosdriver.RunnerIdentity{RunnerID: "old", TargetUDID: "DEV-1"}, false},
		{"simulator", iosdriver.ExpectedRunner{UDID: "SIM-1", Kind: iosdriver.KindSimulator, RunnerID: "new"}, iosdriver.RunnerIdentity{RunnerID: "old", TargetUDID: "SIM-1"}, false},
	}
	for _, tc := range cases {
		if got := tc.want.IsPreviousDeviceRunner(tc.got); got != tc.is {
			t.Errorf("%s: IsPreviousDeviceRunner = %v, want %v", tc.name, got, tc.is)
		}
	}
}
