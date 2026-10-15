//go:build e2e

package iosdriver_test

import (
	"os"
	"testing"

	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/iosdriver"
)

// iosE2EControlPort is the fixed port the Swift XCUITest runner
// listens on, matching device.DriverFactory's wiring.
const iosE2EControlPort = 22087

// TestIosDriver_E2E runs the shared driver contract suite against a
// real, physically connected iOS device. It only runs under the e2e
// build tag with ATOMYX_DRIVER_E2E selecting "ios" and a connected
// device present; see drivertest.RequireDevice. ATOMYX_IOS_DEVICE_UDID
// selects which device to target. It wires Settings as the launchable
// app, since every device ships it, plus the two fault hooks. The
// install, clearable, and focus fixtures need the demo app on the
// device, so those cases skip here until a signed demo build exists.
func TestIosDriver_E2E(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformIOS)
	udid := os.Getenv("ATOMYX_IOS_DEVICE_UDID")
	if udid == "" {
		drivertest.SkipBecause(t, drivertest.SkipRequiredEnvUnset, "set ATOMYX_IOS_DEVICE_UDID to the target device's UDID")
	}

	fx := &iosE2EFixture{udid: udid, kind: iosdriver.KindDevice}
	drivertest.Run(t, fx.newDriver, fx.faultHooks(drivertest.Hooks{LaunchableAppID: "com.apple.Preferences"}))
}

// TestIosDriver_E2E_Simulator runs the same suite against a booted
// simulator with a runner served on the control port. The demo app is
// the launchable, clearable, and install fixture, installed from
// ATOMYX_IOS_INSTALL_APP. FocusInput is unwired until the driver can
// launch with arguments. ATOMYX_IOS_SIM_UDID selects the simulator.
func TestIosDriver_E2E_Simulator(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformIOSSim)
	udid := os.Getenv("ATOMYX_IOS_SIM_UDID")
	if udid == "" {
		drivertest.SkipBecause(t, drivertest.SkipRequiredEnvUnset, "set ATOMYX_IOS_SIM_UDID to the target simulator's UDID")
	}

	fx := &iosE2EFixture{udid: udid, kind: iosdriver.KindSimulator}
	hooks := simulatorDemoHooks(t, udid)
	t.Cleanup(func() { fx.releaseTrackedApp(hooks.LaunchableAppID) })
	drivertest.Run(t, fx.newDriver, fx.faultHooks(hooks))
}
