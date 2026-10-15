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

	// DeviceCalls, when set, returns the running count of round trips
	// the driver instance the current sub-test is exercising has made
	// to the device or agent. The AgentInfo sub-test compares the count
	// before and after reading AgentInfo to prove it never reaches the
	// device.
	//
	// A nil DeviceCalls skips that comparison. A caller whose fake
	// transport cannot count requests, or an end-to-end suite against
	// a real device, leaves it nil and still gets the stability checks.
	DeviceCalls func() int

	// FailConnect, when set, must arrange for the very next Connect
	// call made against the driver instance the current sub-test is
	// exercising to return an error, even though an earlier Connect on
	// the same instance succeeded. The sub-test discards the instance
	// afterward, so FailConnect does not need to self-heal.
	//
	// A nil FailConnect skips the assertion that AgentInfo is the zero
	// value after a failed Connect. A caller whose fake cannot make
	// Connect fail, or an end-to-end suite against a real device, leaves
	// it nil.
	FailConnect func()

	// FocusInput, when set, must leave an editable input focused on
	// the connected driver d, so an erase-text call has text to act
	// on. It may call t.Skip or t.Fatalf. A nil FocusInput skips the
	// erase-text assertion for a driver that reports CanEraseText,
	// since a real screen has no focused input by default. The
	// rejection check for a driver reporting CanEraseText false runs
	// without it.
	FocusInput func(t *testing.T, d driver.Driver)

	// NoForeground, when set, must bring the connected driver d to a
	// state where CurrentForeground reports no app. A nil
	// NoForeground skips the exact-none assertions in the launch
	// lifecycle case, because a real device always has some app or
	// launcher in front. The case still asserts that the launched app
	// is foreground after launch and no longer foreground after stop.
	NoForeground func(t *testing.T, d driver.Driver)

	// LaunchableAppID is an app id that is installed on the target
	// and can be launched, stopped, and relaunched. An empty value
	// skips the launch lifecycle, fresh launch, and attach launch
	// cases.
	LaunchableAppID string

	// ClearableAppID is an app id installed on the target whose data
	// may be wiped. An empty value skips the clear app data case.
	ClearableAppID string

	// Install describes an artifact the install and uninstall
	// roundtrip may install and remove. A nil Install skips that
	// case. The app may already be installed. The case removes it,
	// installs it, removes it again, and reinstalls it from Source
	// afterward when it was present before.
	Install *InstallFixture
}

// InstallFixture names an installable artifact and the app id it
// installs as.
type InstallFixture struct {
	// Source is the value passed to Driver.InstallApp, such as an
	// artifact path.
	Source string
	// AppID is the id Driver.IsAppInstalled and Driver.UninstallApp
	// use for the installed app.
	AppID string
}

// tracked wraps newDriver so every driver it returns is disconnected
// when t finishes. A real adapter can hold an exclusive client slot
// (the iOS runner accepts one client at a time), so a case that
// connects and returns would otherwise block every later case.
// Disconnect is idempotent, so a case that disconnects itself is
// unaffected.
func tracked(t *testing.T, newDriver Factory) Factory {
	return func() driver.Driver {
		d := newDriver()
		t.Cleanup(func() { _ = d.Disconnect() })
		return d
	}
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
	t.Run("platform is non-empty", func(t *testing.T) { testPlatformNonEmpty(t, tracked(t, newDriver)) })
	t.Run("capabilities is stable across calls", func(t *testing.T) { testCapabilitiesStable(t, tracked(t, newDriver)) })
	t.Run("connect lifecycle", func(t *testing.T) { testConnectLifecycle(t, tracked(t, newDriver)) })
	t.Run("disconnect is idempotent", func(t *testing.T) { testDisconnectIdempotent(t, tracked(t, newDriver)) })
	t.Run("ping reports liveness", func(t *testing.T) { testPingReportsLiveness(t, tracked(t, newDriver), hooks) })
	t.Run("agent info is local and stable", func(t *testing.T) { testAgentInfoIsLocalAndStable(t, tracked(t, newDriver), hooks) })
	t.Run("agent info is zero after a failed connect", func(t *testing.T) { testAgentInfoZeroAfterFailedConnect(t, tracked(t, newDriver), hooks) })
	t.Run("context cancellation", func(t *testing.T) { testContextCancellation(t, tracked(t, newDriver)) })

	t.Run("tap long press swipe succeed", func(t *testing.T) { testTapLongPressSwipeSucceed(t, tracked(t, newDriver)) })
	t.Run("dispatch gesture multi-pointer capability", func(t *testing.T) { testDispatchGestureMultiPointerCapability(t, tracked(t, newDriver)) })
	t.Run("dispatch gesture pressure capability", func(t *testing.T) { testDispatchGesturePressureCapability(t, tracked(t, newDriver)) })
	t.Run("dispatch gesture capture capability", func(t *testing.T) { testDispatchGestureCaptureCapability(t, tracked(t, newDriver)) })
	t.Run("erase text capability", func(t *testing.T) { testEraseTextCapability(t, tracked(t, newDriver), hooks) })
	t.Run("hide keyboard capability", func(t *testing.T) { testHideKeyboardCapability(t, tracked(t, newDriver), hooks) })
	t.Run("press key supported code", func(t *testing.T) { testPressKeySupportedCode(t, tracked(t, newDriver), hooks) })
	t.Run("press key unsupported code handled gracefully", func(t *testing.T) { testPressKeyUnsupportedCodeHandledGracefully(t, tracked(t, newDriver)) })
	t.Run("streaming touch is optional", func(t *testing.T) { testStreamingTouchOptional(t, tracked(t, newDriver)) })

	t.Run("foreign automation is optional", func(t *testing.T) { testForeignAutomationOptional(t, tracked(t, newDriver)) })

	t.Run("screen size is positive and stable", func(t *testing.T) { testScreenSizePositiveAndStable(t, tracked(t, newDriver)) })
	t.Run("hierarchy shape", func(t *testing.T) { testHierarchyShape(t, tracked(t, newDriver)) })
	t.Run("hierarchy attributes are canonical", func(t *testing.T) { testHierarchyAttributesAreCanonical(t, tracked(t, newDriver)) })
	t.Run("device info contract", func(t *testing.T) { testDeviceInfoContract(t, tracked(t, newDriver)) })
	t.Run("screenshot capability", func(t *testing.T) { testScreenshotCapability(t, tracked(t, newDriver)) })

	t.Run("launch app lifecycle", func(t *testing.T) { testLaunchAppLifecycle(t, tracked(t, newDriver), hooks) })
	t.Run("launch app fresh stops then launches", func(t *testing.T) { testLaunchAppFreshStopsThenLaunches(t, tracked(t, newDriver), hooks) })
	t.Run("launch app attach reuses foreground", func(t *testing.T) { testLaunchAppAttach(t, tracked(t, newDriver), hooks) })
	t.Run("clear app data succeeds with a source", func(t *testing.T) { testClearAppDataSucceedsWithSource(t, tracked(t, newDriver), hooks) })
	t.Run("install uninstall roundtrip", func(t *testing.T) { testInstallUninstallRoundtrip(t, tracked(t, newDriver), hooks) })
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
		SkipBecause(t, SkipNoFailPing, "no FailPing hook wired for this driver")
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

// testAgentInfoIsLocalAndStable asserts that AgentInfo is readable
// before Connect, returns the same value on every call, and, when
// hooks.DeviceCalls is wired, makes no round trip to the device.
func testAgentInfoIsLocalAndStable(t *testing.T, newDriver Factory, hooks Hooks) {
	d := newDriver()
	before := d.AgentInfo()
	if before != d.AgentInfo() {
		t.Fatal("AgentInfo() before Connect changed between calls, want a stable value")
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = d.Disconnect() }()

	first := d.AgentInfo()
	calls := func() int { return 0 }
	if hooks.DeviceCalls != nil {
		calls = hooks.DeviceCalls
	}
	callsBefore := calls()
	for i := 0; i < 3; i++ {
		if got := d.AgentInfo(); got != first {
			t.Fatalf("AgentInfo() call %d = %+v, want the stable value %+v", i, got, first)
		}
	}
	if after := calls(); after != callsBefore {
		t.Fatalf("AgentInfo() made %d device calls, want none", after-callsBefore)
	}
}

// testAgentInfoZeroAfterFailedConnect asserts that a Connect that
// fails leaves AgentInfo as the zero value, even when an earlier
// Connect on the same instance reported versions.
func testAgentInfoZeroAfterFailedConnect(t *testing.T, newDriver Factory, hooks Hooks) {
	if hooks.FailConnect == nil {
		SkipBecause(t, SkipNoFailConnect, "no FailConnect hook wired for this driver")
	}
	d := newDriver()
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = d.Disconnect()

	hooks.FailConnect()
	if err := d.Connect(context.Background()); err == nil {
		t.Fatal("Connect() after FailConnect returned no error, want the injected failure surfaced")
	}
	if got := d.AgentInfo(); got != (driver.AgentInfo{}) {
		t.Fatalf("AgentInfo() after a failed Connect = %+v, want the zero value", got)
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
