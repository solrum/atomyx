//go:build e2e

package iossimdriver_test

import (
	"context"
	"os"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/iossimdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// iosSimE2EControlPort is the fixed port the Swift XCUITest runner
// listens on, matching device.DriverFactory's wiring.
const iosSimE2EControlPort = 22087

// TestIosSimDriver_E2E runs the shared driver contract suite against
// a real, booted iOS simulator, driven through the atomyx-sim-hid
// helper this adapter's touch path depends on. It only runs under the
// e2e build tag with ATOMYX_DRIVER_E2E selecting "iossim" and a
// booted simulator present; see drivertest.RequireDevice.
// ATOMYX_IOS_SIM_UDID selects which simulator to target. It passes an
// empty drivertest.Hooks: a real booted simulator has no on-demand way
// to go offline, so the suite's ping-failure assertion skips here and
// runs only against the fake-transport contract test in
// driver_test.go.
func TestIosSimDriver_E2E(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformIOSSim)
	udid := os.Getenv("ATOMYX_IOS_SIM_UDID")
	if udid == "" {
		t.Skip("skipping: set ATOMYX_IOS_SIM_UDID to the target simulator's UDID")
	}

	drivertest.Run(t, func() driver.Driver {
		launcher := iossimdriver.NewSimHidLauncher(process.NewExecBackground())
		result, err := launcher.Launch(context.Background(), udid)
		if err != nil {
			t.Fatalf("SimHidLauncher.Launch: %v", err)
		}
		t.Cleanup(func() { _ = result.Kill() })

		conn := transport.NewTCPClient("127.0.0.1", iosSimE2EControlPort)
		d, err := iossimdriver.New(conn, process.NewExec(), iossimdriver.NewDialer(), iossimdriver.Options{
			Options: iosdriver.Options{UDID: udid, Kind: iosdriver.KindSimulator, Port: iosSimE2EControlPort},
			HidPort: result.Port,
		})
		if err != nil {
			t.Fatalf("iossimdriver.New: %v", err)
		}
		return d
	}, drivertest.Hooks{})
}
