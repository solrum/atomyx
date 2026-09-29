//go:build e2e

package androiddriver_test

import (
	"os"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// androidE2EControlPort is the fixed port the Android agent listens
// on, matching device.DriverFactory's wiring.
const androidE2EControlPort = 8765

// TestAndroidDriver_E2E runs the shared driver contract suite against
// a real Android device or emulator reachable through adb. It only
// runs under the e2e build tag with ATOMYX_DRIVER_E2E selecting
// "android" and a connected device present; see
// drivertest.RequireDevice. ATOMYX_ANDROID_SERIAL selects which
// device from "adb devices" to target. It passes an empty
// drivertest.Hooks: a real device or emulator has no on-demand way to
// go offline, so the suite's ping-failure assertion skips here and
// runs only against the fake-transport contract test in
// driver_test.go.
func TestAndroidDriver_E2E(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformAndroid)
	serial := os.Getenv("ATOMYX_ANDROID_SERIAL")
	if serial == "" {
		t.Skip("skipping: set ATOMYX_ANDROID_SERIAL to the target device's adb serial")
	}

	drivertest.Run(t, func() driver.Driver {
		client := transport.NewHTTPClient("127.0.0.1", androidE2EControlPort)
		return androiddriver.New(client, process.NewExec(), androiddriver.Options{
			Serial:     serial,
			HostPort:   androidE2EControlPort,
			DevicePort: androidE2EControlPort,
		})
	}, drivertest.Hooks{})
}
