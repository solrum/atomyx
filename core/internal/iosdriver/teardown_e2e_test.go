//go:build e2e

package iosdriver_test

import (
	"context"
	"os"
	"testing"

	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// TestIosDriver_E2E_Teardown_DisconnectStopsTheTunnelItStarted connects
// a real driver through a recording background runner and checks that
// Disconnect leaves no process the driver started still running. The
// USB tunnel is the only background process a physical-device driver
// starts, and Disconnect owns it. The runner on the device outlives
// the connection by design and is not a driver-started process.
func TestIosDriver_E2E_Teardown_DisconnectStopsTheTunnelItStarted(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformIOS)
	udid := os.Getenv("ATOMYX_IOS_DEVICE_UDID")
	if udid == "" {
		t.Skip("skipping: set ATOMYX_IOS_DEVICE_UDID to the target device's UDID")
	}

	background := &recordingBackground{inner: process.NewExecBackground()}
	d := iosdriver.New(transport.NewTCPClient("127.0.0.1", iosE2EControlPort), process.NewExec(), iosdriver.Options{
		Background: background,
		UDID:       udid,
		Kind:       iosdriver.KindDevice,
		Port:       iosE2EControlPort,
	})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := len(background.processes()); got == 0 {
		t.Fatal("Connect started no background process, so the teardown check would prove nothing")
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	background.requireAllExited(t)
}
