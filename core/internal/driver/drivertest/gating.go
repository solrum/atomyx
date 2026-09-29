package drivertest

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// discoveryTimeout bounds each SDK discovery command (adb devices,
// xcrun simctl list, xcrun devicectl list): all three normally return
// well under a second, so a command still running past this is stuck
// rather than merely slow, and RequireDevice must not hang a test run
// waiting on it.
const discoveryTimeout = 10 * time.Second

// Device-gated e2e platform labels RequireDevice accepts. These are
// independent of driver.Driver.Platform(), which reports "ios" for
// both the physical-device and simulator adapters: the e2e suite
// still needs to select between the two separately, since each needs
// a different SDK check and targets a different kind of real device.
const (
	E2EPlatformAndroid = "android"
	E2EPlatformIOS     = "ios"
	E2EPlatformIOSSim  = "iossim"
)

// e2eEnvVar is the environment variable that opts a platform into the
// device-gated e2e suite: a comma-separated list of the platform
// labels above, or "all" to run every platform whose SDK and a real
// target are discoverable on the host. Run this suite with:
//
//	ATOMYX_DRIVER_E2E=android go test -tags e2e ./internal/androiddriver/...
//	ATOMYX_DRIVER_E2E=all go test -tags e2e ./internal/...
const e2eEnvVar = "ATOMYX_DRIVER_E2E"

// RequireDevice skips the calling test unless platform is selected
// via ATOMYX_DRIVER_E2E and that platform's SDK and a real device or
// simulator are discoverable on the host. It is the gate every e2e
// test file under a //go:build e2e file calls before constructing a
// real-device factory, so a build with the e2e tag still skips
// cleanly instead of failing for lack of hardware when
// ATOMYX_DRIVER_E2E is unset, matching a normal CI run that never
// sets it.
func RequireDevice(t *testing.T, platform string) {
	t.Helper()
	selected := os.Getenv(e2eEnvVar)
	if selected == "" {
		t.Skipf("skipping device-gated e2e test: set %s=%s (or %s=all) to run it", e2eEnvVar, platform, e2eEnvVar)
	}
	if selected != "all" && !selectsPlatform(selected, platform) {
		t.Skipf("skipping device-gated e2e test: %s=%q does not select %q", e2eEnvVar, selected, platform)
	}
	switch platform {
	case E2EPlatformAndroid:
		requireAndroidDevice(t)
	case E2EPlatformIOSSim:
		requireIOSSimulator(t)
	case E2EPlatformIOS:
		requireIOSDevice(t)
	default:
		t.Fatalf("RequireDevice: unknown platform %q", platform)
	}
}

func selectsPlatform(list, platform string) bool {
	for _, p := range strings.Split(list, ",") {
		if strings.TrimSpace(p) == platform {
			return true
		}
	}
	return false
}

func requireAndroidDevice(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("adb"); err != nil {
		t.Skip("skipping: adb not found on PATH, install the Android SDK platform-tools")
	}
	ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "adb", "devices").Output()
	if err != nil {
		t.Skipf("skipping: adb devices failed: %v", err)
	}
	if !hasConnectedAndroidDevice(string(out)) {
		t.Skip("skipping: no Android device or emulator is connected (adb devices reported none)")
	}
}

// hasConnectedAndroidDevice reports whether raw, the output of "adb
// devices", lists at least one entry in the "ready" state. "adb
// devices" prints a header line, then one "<serial>\t<state>" line
// per known device; a state other than "device" (for example
// "offline" or "unauthorized") is not usable.
func hasConnectedAndroidDevice(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") {
			continue
		}
		if strings.HasSuffix(line, "\tdevice") {
			return true
		}
	}
	return false
}

func requireIOSSimulator(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("skipping: xcrun not found on PATH, install the Xcode command line tools")
	}
	ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "list", "devices", "booted").Output()
	if err != nil {
		t.Skipf("skipping: xcrun simctl list devices failed: %v", err)
	}
	if !strings.Contains(string(out), "Booted") {
		t.Skip("skipping: no booted iOS simulator found (xcrun simctl list devices booted reported none)")
	}
}

func requireIOSDevice(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("skipping: xcrun not found on PATH, install the Xcode command line tools")
	}
	ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "devicectl", "list", "devices").Output()
	if err != nil {
		t.Skipf("skipping: xcrun devicectl list devices failed: %v", err)
	}
	if !strings.Contains(string(out), "connected") {
		t.Skip("skipping: no physical iOS device is connected (xcrun devicectl list devices reported none)")
	}
}
