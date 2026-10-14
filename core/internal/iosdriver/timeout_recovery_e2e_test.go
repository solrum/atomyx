//go:build e2e

package iosdriver_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

const (
	// e2eClientBound is the client's own fallback bound, short enough
	// that the held reply outlasts it.
	e2eClientBound = 500 * time.Millisecond
	// e2eHoldDuration is how long the proxy holds the chosen reply.
	e2eHoldDuration = 3 * e2eClientBound
)

// TestIosDriver_E2E_TimeoutRecovery_HeldReplyIsToolTimeoutAndNextCallSucceeds
// runs the timeout recovery case against a physical device. See
// requireTimeoutRecovery for what it asserts.
func TestIosDriver_E2E_TimeoutRecovery_HeldReplyIsToolTimeoutAndNextCallSucceeds(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformIOS)
	udid := os.Getenv("ATOMYX_IOS_DEVICE_UDID")
	if udid == "" {
		t.Skip("skipping: set ATOMYX_IOS_DEVICE_UDID to the target device's UDID")
	}
	requireTimeoutRecovery(t, udid, iosdriver.KindDevice)
}

// TestIosDriver_E2E_SimulatorTimeoutRecovery_HeldReplyIsToolTimeoutAndNextCallSucceeds
// runs the same case against a booted simulator. The simulator driver
// shares this adapter's transport path for every non-touch call, so
// the case needs no touch helper.
func TestIosDriver_E2E_SimulatorTimeoutRecovery_HeldReplyIsToolTimeoutAndNextCallSucceeds(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformIOSSim)
	udid := os.Getenv("ATOMYX_IOS_SIM_UDID")
	if udid == "" {
		t.Skip("skipping: set ATOMYX_IOS_SIM_UDID to the target simulator's UDID")
	}
	requireTimeoutRecovery(t, udid, iosdriver.KindSimulator)
}

// requireTimeoutRecovery puts a delay proxy between the driver and the
// real runner, holds the reply to the first screen size request past the
// client's own bound, and checks the call fails with TOOL_TIMEOUT and
// that the next calls succeed against the same runner. The call
// carries no context deadline, so the classification comes from the
// transport's own fallback bound.
func requireTimeoutRecovery(t *testing.T, udid string, kind iosdriver.Kind) {
	t.Helper()
	proxyPort, delivered := drivertest.NewLineDelayProxy(t, fmt.Sprintf("127.0.0.1:%d", iosE2EControlPort), "getScreenSize", e2eHoldDuration)
	conn := transport.NewTCPClient("127.0.0.1", proxyPort, transport.WithTCPResponseTimeout(e2eClientBound))
	d := iosdriver.New(conn, process.NewExec(), iosdriver.Options{
		Background: process.NewExecBackground(),
		UDID:       udid,
		Kind:       kind,
		Port:       iosE2EControlPort,
	})
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = d.Disconnect() })

	_, err := d.ScreenSize(ctx)
	if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
		t.Fatalf("held ScreenSize error = %v (code %q), want code %q", err, code, diag.CodeToolTimeout)
	}
	if err := d.Ping(ctx); err != nil {
		t.Fatalf("Ping after the timeout: %v", err)
	}
	if _, err := d.ScreenSize(ctx); err != nil {
		t.Fatalf("ScreenSize after the timeout: %v", err)
	}
	select {
	case <-delivered:
	case <-time.After(e2eHoldDuration + e2eClientBound):
		t.Fatal("the held reply was neither delivered nor dropped in time")
	}
	size, err := d.ScreenSize(ctx)
	if err != nil {
		t.Fatalf("ScreenSize after the late reply: %v", err)
	}
	if size.Width <= 0 || size.Height <= 0 {
		t.Fatalf("ScreenSize after the late reply = %+v, want positive dimensions from the matching reply", size)
	}
}
