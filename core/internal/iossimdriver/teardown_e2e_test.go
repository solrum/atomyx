//go:build e2e

package iossimdriver_test

import (
	"context"
	"os"
	"testing"

	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// TestIosSimDriver_E2E_Teardown_DisconnectStopsWhatTheDriverStarted
// connects a real simulator driver and checks which background
// processes are gone after Disconnect.
//
// Ownership of the two process sets:
//
//   - Started by the driver through its own background runner:
//     Disconnect must stop every one. A simulator driver starts none,
//     because the XCUITest runner outlives the connection and no
//     tunnel is needed.
//   - The atomyx-sim-hid helper: the caller launches it and passes the
//     port in, so Disconnect only closes the driver's connection to it
//     and the caller's cleanup stops the process. The case reports
//     whether the helper is still running after Disconnect, then
//     checks that the cleanup stops it.
func TestIosSimDriver_E2E_Teardown_DisconnectStopsWhatTheDriverStarted(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformIOSSim)
	udid := os.Getenv("ATOMYX_IOS_SIM_UDID")
	if udid == "" {
		t.Skip("skipping: set ATOMYX_IOS_SIM_UDID to the target simulator's UDID")
	}

	driverBackground := &recordingBackground{inner: process.NewExecBackground()}
	hidBackground := &recordingBackground{inner: process.NewExecBackground()}
	t.Cleanup(func() { hidBackground.requireAllExited(t) })
	hidPort := startSimHid(t, udid, hidBackground)
	d := newSimDriver(t, transport.NewTCPClient("127.0.0.1", iosSimE2EControlPort), udid, driverBackground, hidPort)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	if started := len(driverBackground.processes()); started != 0 {
		t.Errorf("the simulator driver started %d background processes, want 0: a process the driver starts must be stopped by Disconnect, so extend this case to assert that before allowing one", started)
	}
	driverBackground.requireAllExited(t)
	t.Logf("sim-hid helper still running after Disconnect: %t (stopped by test cleanup, not by Disconnect)", hidBackground.running())
}
