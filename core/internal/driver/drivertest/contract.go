package drivertest

import (
	"context"
	"reflect"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

// Factory constructs a fresh driver.Driver instance for one test
// case. Run calls it once per sub-test so no state leaks between
// assertions.
type Factory func() driver.Driver

// Stager is implemented by a driver.Driver value that can preload a
// hierarchy tree before Hierarchy is called. A hierarchy-content
// assertion type-asserts the subject against this interface and
// skips when the subject does not implement it, since staging a tree
// is a test-only capability outside the Driver interface itself.
type Stager interface {
	StageHierarchyForContract(tree wire.TreeNodeWire)
}

// Hooks carries optional test-only injection points that a driver
// adapter's contract-test caller wires to its own fake transport, so
// the shared suite can exercise behavior that a portable call through
// the Driver interface cannot trigger on its own.
type Hooks struct {
	// FailPing, when set, must arrange for the very next Ping call
	// made against the driver instance the current sub-test is
	// exercising to return a typed unreachable diagnostic. The
	// sub-test that uses it issues exactly one Ping call afterward and
	// then discards the driver instance, so FailPing does not need to
	// self-heal; it only needs to guarantee that next call fails.
	//
	// A nil FailPing skips the ping-failure assertion. Every fake-
	// transport-backed caller in this repo wires it; the exception is
	// an end-to-end suite driving a real device or agent process,
	// which has no on-demand way to take that process offline.
	FailPing func()
}

// Run executes the driver contract suite against every driver value
// newDriver constructs. It is the single suite any Driver
// implementation is checked against, regardless of which platform
// it wraps.
//
// A sub-test that needs a specific pre-launch state (an installed
// app, a foreground app, a staged hierarchy) drives it through the
// Driver interface itself wherever possible, so the suite exercises
// the same contract a real adapter must honor. Hierarchy-content
// assertions that require a specific tree shape are the one
// exception: staging one is a test-only capability outside the
// Driver interface, so that sub-test skips when the subject offers
// no way to stage one (see Stager).
func Run(t *testing.T, newDriver Factory, hooks Hooks) {
	t.Helper()
	t.Run("platform is non-empty", func(t *testing.T) { testPlatformNonEmpty(t, newDriver) })
	t.Run("capabilities is stable across calls", func(t *testing.T) { testCapabilitiesStable(t, newDriver) })
	t.Run("connect lifecycle", func(t *testing.T) { testConnectLifecycle(t, newDriver) })
	t.Run("disconnect is idempotent", func(t *testing.T) { testDisconnectIdempotent(t, newDriver) })
	t.Run("ping reports liveness", func(t *testing.T) { testPingReportsLiveness(t, newDriver, hooks) })
	t.Run("context cancellation", func(t *testing.T) { testContextCancellation(t, newDriver) })

	t.Run("tap long press swipe succeed", func(t *testing.T) { testTapLongPressSwipeSucceed(t, newDriver) })
	t.Run("dispatch gesture multi-pointer capability", func(t *testing.T) { testDispatchGestureMultiPointerCapability(t, newDriver) })
	t.Run("dispatch gesture pressure capability", func(t *testing.T) { testDispatchGesturePressureCapability(t, newDriver) })
	t.Run("dispatch gesture capture capability", func(t *testing.T) { testDispatchGestureCaptureCapability(t, newDriver) })
	t.Run("erase text capability", func(t *testing.T) { testEraseTextCapability(t, newDriver) })
	t.Run("hide keyboard capability", func(t *testing.T) { testHideKeyboardCapability(t, newDriver) })
	t.Run("press key supported code", func(t *testing.T) { testPressKeySupportedCode(t, newDriver) })
	t.Run("press key unsupported code handled gracefully", func(t *testing.T) { testPressKeyUnsupportedCodeHandledGracefully(t, newDriver) })
	t.Run("streaming touch is optional", func(t *testing.T) { testStreamingTouchOptional(t, newDriver) })

	t.Run("screen size is positive and stable", func(t *testing.T) { testScreenSizePositiveAndStable(t, newDriver) })
	t.Run("hierarchy shape", func(t *testing.T) { testHierarchyShape(t, newDriver) })
	t.Run("hierarchy attributes are canonical", func(t *testing.T) { testHierarchyAttributesAreCanonical(t, newDriver) })
	t.Run("device info contract", func(t *testing.T) { testDeviceInfoContract(t, newDriver) })
	t.Run("screenshot capability", func(t *testing.T) { testScreenshotCapability(t, newDriver) })

	t.Run("launch app lifecycle", func(t *testing.T) { testLaunchAppLifecycle(t, newDriver) })
	t.Run("launch app fresh stops then launches", func(t *testing.T) { testLaunchAppFreshStopsThenLaunches(t, newDriver) })
	t.Run("launch app attach reuses foreground", func(t *testing.T) { testLaunchAppAttach(t, newDriver) })
	t.Run("clear app data succeeds with a source", func(t *testing.T) { testClearAppDataSucceedsWithSource(t, newDriver) })
	t.Run("install uninstall roundtrip", func(t *testing.T) { testInstallUninstallRoundtrip(t, newDriver) })
}

func testPlatformNonEmpty(t *testing.T, newDriver Factory) {
	d := newDriver()
	if d.Platform() == "" {
		t.Fatal("Platform() returned an empty string")
	}
}

func testCapabilitiesStable(t *testing.T, newDriver Factory) {
	d := newDriver()
	first := d.Capabilities()
	second := d.Capabilities()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Capabilities() changed between calls with no intervening state change: %+v vs %+v", first, second)
	}
}

func testConnectLifecycle(t *testing.T, newDriver Factory) {
	d := newDriver()
	if d.IsConnected() {
		t.Fatal("IsConnected() is true before Connect")
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !d.IsConnected() {
		t.Fatal("IsConnected() is false after Connect")
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if d.IsConnected() {
		t.Fatal("IsConnected() is true after Disconnect")
	}
}

func testDisconnectIdempotent(t *testing.T, newDriver Factory) {
	d := newDriver()
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect before Connect: %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("first Disconnect: %v", err)
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("second Disconnect: %v", err)
	}
}

// testPingReportsLiveness asserts that Ping reports a connected
// driver reachable, then, when hooks.FailPing is set, forces the next
// Ping call to fail and asserts the failure is describable through
// diag.Describe as diag.CodeAgentUnreachable. A nil hooks.FailPing
// skips the failure assertion, since forcing an unreachable agent has
// no portable way to express against every adapter's own transport
// from inside this shared suite.
func testPingReportsLiveness(t *testing.T, newDriver Factory, hooks Hooks) {
	d := newDriver()
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = d.Disconnect() }()

	if err := d.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() on a connected driver: %v", err)
	}

	if hooks.FailPing == nil {
		t.Skip("drivertest: no FailPing hook wired for this driver; skipping the unreachable-ping assertion")
	}
	hooks.FailPing()
	err := d.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping() after FailPing returned no error, want the injected failure surfaced")
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
		t.Fatalf("Ping() after FailPing code = %q, want %q", code, diag.CodeAgentUnreachable)
	}
}

// testContextCancellation asserts that every Driver method taking a
// context.Context rejects a call made with an already-canceled one:
// an adapter with in-flight or pending I/O must observe cancellation
// rather than complete the call.
func testContextCancellation(t *testing.T, newDriver Factory) {
	cases := []struct {
		name string
		call func(ctx context.Context, d driver.Driver) error
	}{
		{"Connect", func(ctx context.Context, d driver.Driver) error { return d.Connect(ctx) }},
		{"Ping", func(ctx context.Context, d driver.Driver) error { return d.Ping(ctx) }},
		{"Hierarchy", func(ctx context.Context, d driver.Driver) error { _, err := d.Hierarchy(ctx); return err }},
		{"WaitForIdle", func(ctx context.Context, d driver.Driver) error { _, err := d.WaitForIdle(ctx, 100); return err }},
		{"Tap", func(ctx context.Context, d driver.Driver) error { return d.Tap(ctx, wire.Point{}) }},
		{"LongPress", func(ctx context.Context, d driver.Driver) error { return d.LongPress(ctx, wire.Point{}, 100) }},
		{"Swipe", func(ctx context.Context, d driver.Driver) error {
			return d.Swipe(ctx, wire.Point{}, wire.Point{X: 1, Y: 1}, 100)
		}},
		{"DispatchGesture", func(ctx context.Context, d driver.Driver) error {
			_, err := d.DispatchGesture(ctx, driver.Gesture{})
			return err
		}},
		{"InputText", func(ctx context.Context, d driver.Driver) error { return d.InputText(ctx, "x") }},
		{"EraseText", func(ctx context.Context, d driver.Driver) error { return d.EraseText(ctx, 1) }},
		{"PressKey", func(ctx context.Context, d driver.Driver) error {
			_, err := d.PressKey(ctx, driver.KeyBack)
			return err
		}},
		{"HideKeyboard", func(ctx context.Context, d driver.Driver) error { _, err := d.HideKeyboard(ctx); return err }},
		{"LaunchApp", func(ctx context.Context, d driver.Driver) error {
			return d.LaunchApp(ctx, "dev.atomyx.drivertest.cancel", driver.LaunchArgs{})
		}},
		{"StopApp", func(ctx context.Context, d driver.Driver) error {
			return d.StopApp(ctx, "dev.atomyx.drivertest.cancel")
		}},
		{"KillApp", func(ctx context.Context, d driver.Driver) error {
			return d.KillApp(ctx, "dev.atomyx.drivertest.cancel")
		}},
		{"CurrentForeground", func(ctx context.Context, d driver.Driver) error {
			_, err := d.CurrentForeground(ctx)
			return err
		}},
		{"ListApps", func(ctx context.Context, d driver.Driver) error { _, err := d.ListApps(ctx); return err }},
		{"InstallApp", func(ctx context.Context, d driver.Driver) error {
			return d.InstallApp(ctx, "dev.atomyx.drivertest.cancel")
		}},
		{"UninstallApp", func(ctx context.Context, d driver.Driver) error {
			return d.UninstallApp(ctx, "dev.atomyx.drivertest.cancel")
		}},
		{"ClearAppData", func(ctx context.Context, d driver.Driver) error {
			return d.ClearAppData(ctx, "dev.atomyx.drivertest.cancel", driver.ClearAppDataOptions{})
		}},
		{"IsAppInstalled", func(ctx context.Context, d driver.Driver) error {
			_, err := d.IsAppInstalled(ctx, "dev.atomyx.drivertest.cancel")
			return err
		}},
		{"Screenshot", func(ctx context.Context, d driver.Driver) error { _, err := d.Screenshot(ctx); return err }},
		{"DeviceInfo", func(ctx context.Context, d driver.Driver) error { _, err := d.DeviceInfo(ctx); return err }},
		{"ScreenSize", func(ctx context.Context, d driver.Driver) error { _, err := d.ScreenSize(ctx); return err }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDriver()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := tc.call(ctx, d); err == nil {
				t.Fatalf("%s with an already-canceled context returned no error", tc.name)
			}
		})
	}
}

// stageMinimalHierarchy stages an empty tree on a subject that
// implements Stager. It reports whether staging happened so the
// caller can skip content assertions on a subject that doesn't.
func stageMinimalHierarchy(subject driver.Driver, tree wire.TreeNodeWire) bool {
	s, ok := subject.(Stager)
	if !ok {
		return false
	}
	s.StageHierarchyForContract(tree)
	return true
}
