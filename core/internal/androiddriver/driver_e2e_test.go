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
// device from "adb devices" to target.
//
// The hooks wire only fixtures a real device supports. The demo app
// "dev.atomyx.demo" must be installed and serves as the launchable and
// clearable app. ATOMYX_ANDROID_INSTALL_APK (an APK path) together
// with ATOMYX_ANDROID_INSTALL_APP_ID (that APK's package name) enable
// the install roundtrip, which removes the package first when it is
// already installed. A real device has no on-demand way to go offline,
// no reliably focusable input, and always shows some app or launcher
// in front, so FailPing, FailConnect, FocusInput, and NoForeground
// stay unset and the cases that need them skip.
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
	}, androidE2EHooks())
}

const androidE2EDemoAppID = "dev.atomyx.demo"

func androidE2EHooks() drivertest.Hooks {
	hooks := drivertest.Hooks{
		LaunchableAppID: androidE2EDemoAppID,
		ClearableAppID:  androidE2EDemoAppID,
	}
	apk := os.Getenv("ATOMYX_ANDROID_INSTALL_APK")
	appID := os.Getenv("ATOMYX_ANDROID_INSTALL_APP_ID")
	if apk != "" && appID != "" {
		hooks.Install = &drivertest.InstallFixture{Source: apk, AppID: appID}
	}
	return hooks
}
