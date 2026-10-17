//go:build e2e

package androiddriver_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

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
// The demo app "dev.atomyx.demo" must be installed and serves as the
// launchable and clearable app. ATOMYX_ANDROID_INSTALL_APK (an APK
// path, absolute or relative to the repository root) together with
// ATOMYX_ANDROID_INSTALL_APP_ID (that APK's package name) enable the
// install roundtrip, which removes the package first when it is already
// installed. A path that does not name a file fails the test before the
// device is touched.
//
// Each driver the suite builds gets its own adb forward on a host port
// the test picked, so the fault hooks only ever touch resources this
// test created. FailConnect points the next Connect at a closed local
// port. FailPing removes the forward the driver created. FocusInput
// opens the demo app's focus route through its deep link. A real device
// always shows some app or launcher in front, so NoForeground stays
// unset and the cases that need it skip.
func TestAndroidDriver_E2E(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformAndroid)
	serial := os.Getenv("ATOMYX_ANDROID_SERIAL")
	if serial == "" {
		drivertest.SkipBecause(t, drivertest.SkipRequiredEnvUnset, "set ATOMYX_ANDROID_SERIAL to the target device's adb serial")
	}

	fx := &androidE2EFixture{serial: serial}
	t.Cleanup(fx.removeCreatedForwards)
	drivertest.Run(t, fx.newDriver, fx.hooks(t))
}

const (
	androidE2EDemoAppID       = "dev.atomyx.demo"
	androidE2EFocusRoute      = "/focus"
	androidE2EFocusLink       = "atomyxdemo://open/focus"
	androidE2EPermissionRoute = "/permission"
	androidE2EGesturesRoute   = "/gestures"
	androidE2ELoginRoute      = "/login"
	androidE2EMainActivity    = "dev.atomyx.demo/.MainActivity"
	androidE2EFocusInput      = "focus-input"
	androidE2EAdbDeadline     = 30 * time.Second
	androidE2EHTTPTimeout     = 10 * time.Second
)

// androidE2EInstance is one driver the suite built, with the command
// runner that recorded the forwards it created and the transport whose
// health probe the connect fault redirects.
type androidE2EInstance struct {
	runner    *recordingRunner
	transport *faultTransport
	// http is the connection pool behind the driver's client. A removed
	// adb forward leaves already established connections open, so a
	// fault that must be seen by the next call also drops idle ones.
	http *http.Transport
}

// androidE2EFixture builds drivers for the corpus and remembers the
// latest one, because the fault hooks act on the instance the current
// sub-test is exercising.
type androidE2EFixture struct {
	serial string

	mu        sync.Mutex
	instances []*androidE2EInstance
}

func (f *androidE2EFixture) newDriver() driver.Driver {
	hostPort := freeLocalPort()
	pool := http.DefaultTransport.(*http.Transport).Clone()
	inst := &androidE2EInstance{
		runner: &recordingRunner{inner: process.NewExec()},
		transport: &faultTransport{
			Transport: transport.NewHTTPClient("127.0.0.1", hostPort,
				transport.WithHTTPClient(&http.Client{Transport: pool, Timeout: androidE2EHTTPTimeout})),
		},
		http: pool,
	}
	f.mu.Lock()
	f.instances = append(f.instances, inst)
	f.mu.Unlock()
	return androiddriver.New(inst.transport, inst.runner, androiddriver.Options{
		Serial:     f.serial,
		HostPort:   hostPort,
		DevicePort: androidE2EControlPort,
	})
}

func (f *androidE2EFixture) current() *androidE2EInstance {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.instances[len(f.instances)-1]
}

func (f *androidE2EFixture) hooks(t *testing.T) drivertest.Hooks {
	t.Helper()
	hooks := drivertest.Hooks{
		LaunchableAppID: androidE2EDemoAppID,
		ClearableAppID:  androidE2EDemoAppID,
		FailConnect:     func() { f.current().transport.breakHealth() },
		FailPing: func() {
			inst := f.current()
			f.removeForwards(inst)
			inst.http.CloseIdleConnections()
		},
		FocusInput: f.focusInput,
		LaunchArguments: &drivertest.LaunchArgumentsFixture{
			Arguments: map[string]string{"route": androidE2EFocusRoute},
			Await: func(t *testing.T, d driver.Driver) {
				drivertest.AwaitFocusedInput(t, d, androidE2EFocusInput)
			},
		},
		OpenLink: &drivertest.OpenLinkFixture{
			Link:  androidE2EFocusLink,
			AppID: androidE2EDemoAppID,
			Await: func(t *testing.T, d driver.Driver) {
				drivertest.AwaitFocusedInput(t, d, androidE2EFocusInput)
			},
		},
		Permissions: &drivertest.PermissionsFixture{
			Permission: driver.PermissionLocation,
			Arguments:  map[string]string{"route": androidE2EPermissionRoute},
			Await:      drivertest.AwaitPermissionStatus,
		},
		Orientation: &drivertest.OrientationFixture{
			Arguments:       map[string]string{"route": androidE2EGesturesRoute},
			SettlePause:     drivertest.RotationAnimationPause,
			TapAndAwait:     drivertest.TapGestureTarget,
			SignInArguments: map[string]string{"route": androidE2ELoginRoute},
			TypeAndAwait:    drivertest.VerifySignInKeyboard,
		},
	}
	appID := os.Getenv("ATOMYX_ANDROID_INSTALL_APP_ID")
	if os.Getenv("ATOMYX_ANDROID_INSTALL_APK") != "" && appID != "" {
		apk := drivertest.RequireInstallSource(t, "ATOMYX_ANDROID_INSTALL_APK", drivertest.InstallSourceFile)
		hooks.Install = &drivertest.InstallFixture{Source: apk, AppID: appID}
	}
	return hooks
}

// focusInput restarts the demo app on its focus route through the
// "route" intent extra, using adb directly because the driver exposes no
// route-aware launch, then waits until the field reports focus. The
// force stop makes the extra take effect even when the app is running.
func (f *androidE2EFixture) focusInput(t *testing.T, d driver.Driver) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), androidE2EAdbDeadline)
	defer cancel()
	exec := process.NewExec()
	if _, err := exec.Run(ctx, "adb", "-s", f.serial, "shell", "am", "force-stop", androidE2EDemoAppID); err != nil {
		t.Fatalf("stop %s through adb: %v", androidE2EDemoAppID, err)
	}
	if _, err := exec.Run(ctx, "adb", "-s", f.serial, "shell", "am", "start",
		"-n", androidE2EMainActivity, "--es", "route", androidE2EFocusRoute); err != nil {
		t.Fatalf("start the demo app on %s through adb: %v", androidE2EFocusRoute, err)
	}
	drivertest.AwaitFocusedInput(t, d, androidE2EFocusInput)
}

// removeForwards removes only the forwards the instance's driver
// created and has not yet removed.
func (f *androidE2EFixture) removeForwards(inst *androidE2EInstance) {
	creates, removals := inst.runner.forwards()
	removed := make(map[string]bool, len(removals))
	for _, r := range removals {
		removed[r] = true
	}
	for _, endpoint := range creates {
		if removed[endpoint] {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), androidE2EAdbDeadline)
		_, _ = inst.runner.inner.Run(ctx, "adb", "-s", f.serial, "forward", "--remove", endpoint)
		cancel()
	}
}

func (f *androidE2EFixture) removeCreatedForwards() {
	f.mu.Lock()
	instances := append([]*androidE2EInstance(nil), f.instances...)
	f.mu.Unlock()
	for _, inst := range instances {
		f.removeForwards(inst)
	}
}

// faultTransport forwards every call to the real transport except the
// health probe, which Connect issues first. After breakHealth the probe
// goes to a client aimed at a closed local port.
type faultTransport struct {
	androiddriver.Transport

	mu     sync.Mutex
	broken androiddriver.Transport
}

func (f *faultTransport) breakHealth() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broken = transport.NewHTTPClient("127.0.0.1", freeLocalPort())
}

func (f *faultTransport) Health(ctx context.Context) (transport.HealthResponse, error) {
	f.mu.Lock()
	broken := f.broken
	f.mu.Unlock()
	if broken != nil {
		return broken.Health(ctx)
	}
	return f.Transport.Health(ctx)
}

// freeLocalPort returns a loopback TCP port with no listener, by
// binding port zero and releasing it.
func freeLocalPort() int {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("find a free local port: %v", err))
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}
