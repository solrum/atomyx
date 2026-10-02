//go:build e2e

package iosdriver_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

// TestXctestLauncher_E2E_SimulatorRunnerReportsItsLaunchIdentity
// launches the real Swift runner on a booted simulator through
// XctestLauncher, on an allocated non-default port with a runner id,
// then proves over the wire that the ping reply carries that identity
// and that a different expected identity is rejected. It needs
// ATOMYX_E2E_IOS_SIM_UDID set to a booted simulator and
// ATOMYX_E2E_IOS_PROJECT_DIR set to platforms/ios-agent with the
// project generated.
func TestXctestLauncher_E2E_SimulatorRunnerReportsItsLaunchIdentity(t *testing.T) {
	udid := os.Getenv("ATOMYX_E2E_IOS_SIM_UDID")
	projectDir := os.Getenv("ATOMYX_E2E_IOS_PROJECT_DIR")
	if udid == "" || projectDir == "" {
		t.Skip("skipping: set ATOMYX_E2E_IOS_SIM_UDID and ATOMYX_E2E_IOS_PROJECT_DIR")
	}
	projectDir, err := filepath.Abs(projectDir)
	if err != nil {
		t.Fatalf("abs project dir: %v", err)
	}

	const runnerID = "e2e-runner-identity"
	allocate := func() (int, error) {
		l, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		defer func() { _ = l.Close() }()
		return l.Addr().(*net.TCPAddr).Port, nil
	}
	launcher := iosdriver.NewXctestLauncher(process.NewExec(), process.NewExecBackground(), clock.SystemClock{}, iosdriver.LaunchConfig{
		ProjectDir:   projectDir,
		Kind:         iosdriver.KindSimulator,
		UDID:         udid,
		RunnerID:     runnerID,
		AllocatePort: allocate,
	})
	kill, err := launcher.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}
	defer func() { _ = kill() }()

	port := launcher.Port()
	if port == 22087 {
		t.Fatalf("runner port = %d, want a non-default port", port)
	}
	got, ok := iosdriver.ProbeRunnerIdentity(context.Background(), port, iosdriver.RunnerProbeTimeout)
	if !ok {
		t.Fatalf("no runner answers ping on port %d", port)
	}
	t.Logf("ping identity on port %d: %+v", port, got)
	if got.RunnerID != runnerID || got.SimulatorUDID != udid {
		t.Fatalf("identity = %+v, want runnerId %q and simulatorUdid %q", got, runnerID, udid)
	}

	for name, want := range map[string]iosdriver.ExpectedRunner{
		"other runner id": {UDID: udid, Kind: iosdriver.KindSimulator, RunnerID: "someone-else"},
		"other simulator": {UDID: "00000000-0000-0000-0000-000000000000", Kind: iosdriver.KindSimulator, RunnerID: runnerID},
	} {
		_, _, idErr := iosdriver.CheckRunnerIdentity(context.Background(), port, iosdriver.RunnerProbeTimeout, want)
		var identityErr *iosdriver.RunnerIdentityError
		if !errors.As(idErr, &identityErr) {
			t.Fatalf("%s: err = %v, want *RunnerIdentityError", name, idErr)
		}
		t.Logf("%s rejected: %v", name, idErr)
	}
	_, _, okErr := iosdriver.CheckRunnerIdentity(context.Background(), port, iosdriver.RunnerProbeTimeout,
		iosdriver.ExpectedRunner{UDID: udid, Kind: iosdriver.KindSimulator, RunnerID: runnerID})
	if okErr != nil {
		t.Fatalf("matching identity rejected: %v", okErr)
	}
}
