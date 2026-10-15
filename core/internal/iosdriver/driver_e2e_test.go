//go:build e2e

package iosdriver_test

import (
	"os"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// iosE2EControlPort is the fixed port the Swift XCUITest runner
// listens on, matching device.DriverFactory's wiring.
const iosE2EControlPort = 22087

// TestIosDriver_E2E runs the shared driver contract suite against a
// real, physically connected iOS device. It only runs under the e2e
// build tag with ATOMYX_DRIVER_E2E selecting "ios" and a connected
// device present; see drivertest.RequireDevice. ATOMYX_IOS_DEVICE_UDID
// selects which device to target. It wires Settings as the launchable
// app, since every device ships it. A real device has no on-demand way
// to go offline and no clearable app id or install artifact known to
// be present, so the cases that need those fixtures skip here and run
// only against the fake-transport contract test in driver_test.go.
func TestIosDriver_E2E(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformIOS)
	udid := os.Getenv("ATOMYX_IOS_DEVICE_UDID")
	if udid == "" {
		drivertest.SkipBecause(t, drivertest.SkipRequiredEnvUnset, "set ATOMYX_IOS_DEVICE_UDID to the target device's UDID")
	}

	drivertest.Run(t, func() driver.Driver {
		conn := transport.NewTCPClient("127.0.0.1", iosE2EControlPort)
		return iosdriver.New(conn, process.NewExec(), iosdriver.Options{
			Background: process.NewExecBackground(),
			UDID:       udid,
			Kind:       iosdriver.KindDevice,
			Port:       iosE2EControlPort,
		})
	}, drivertest.Hooks{LaunchableAppID: "com.apple.Preferences"})
}
