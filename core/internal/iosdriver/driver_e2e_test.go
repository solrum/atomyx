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
// selects which device to target. The signed demo app is the
// launchable, clearable, and install fixture, installed from
// ATOMYX_IOS_INSTALL_APP, and FocusInput, the launch arguments
// fixture, the link fixture, and the orientation fixture drive it on
// its routes. The two fault hooks are wired as well.
func TestIosDriver_E2E(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformIOS)
	udid := os.Getenv("ATOMYX_IOS_DEVICE_UDID")
	if udid == "" {
		drivertest.SkipBecause(t, drivertest.SkipRequiredEnvUnset, "set ATOMYX_IOS_DEVICE_UDID to the target device's UDID")
	}

	fx := &iosE2EFixture{udid: udid, kind: iosdriver.KindDevice}
	hooks := deviceDemoHooks(t, udid)
	t.Cleanup(func() { fx.releaseTrackedApp(hooks.LaunchableAppID) })
	drivertest.Run(t, fx.newDriver, fx.faultHooks(hooks))
}

// TestIosDriver_E2E_Simulator runs the same suite against a booted
// simulator with a runner served on the control port. The demo app is
// the launchable, clearable, and install fixture, installed from
// ATOMYX_IOS_INSTALL_APP. FocusInput and the launch arguments fixture
// start the demo app on its focus route through launch arguments.
// ATOMYX_IOS_SIM_UDID selects the simulator.
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
