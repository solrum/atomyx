package drivertest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

const (
	// orientationRestoreTimeout bounds the cleanup that turns the device
	// back to portrait after the case, whatever the case did.
	orientationRestoreTimeout = 60 * time.Second

	// RotationAnimationPause is the settle time a fixture for a real
	// device asks for. It lets the rotation animation finish after the
	// screen size already reports the new layout, and covers the turn
	// between the two landscape orientations, which the size cannot
	// show.
	RotationAnimationPause = 1500 * time.Millisecond

	// tapCountSettleTimeout bounds how long TapGestureTarget waits for
	// the demo app's tap counter to show the tap.
	tapCountSettleTimeout = 15 * time.Second

	// tapCountPoll is the gap between hierarchy reads while waiting.
	tapCountPoll = 250 * time.Millisecond

	// tapTargetID is the id of the demo app's tap button.
	tapTargetID = "gesture-tap-target"

	// tapCountPrefix starts the label the demo app's tap counter shows.
	tapCountPrefix = "Tap count: "
)

// OrientationFixture describes an app screen with a known tap target, so
// a case can prove that a gesture still lands where the hierarchy says
// after the device rotates.
type OrientationFixture struct {
	// Arguments is handed to Driver.LaunchApp on a fresh launch of
	// Hooks.LaunchableAppID to open the screen.
	Arguments map[string]string

	// SettlePause is how long the case waits after each rotation before
	// it taps. A fixture for a real device sets RotationAnimationPause,
	// and a synthetic one leaves it zero.
	SettlePause time.Duration

	// TapAndAwait must read the tap target's position from the current
	// hierarchy, tap its center, and block until the screen shows that
	// the tap landed, failing the test when it does not. It runs in
	// every orientation the case visits, so it must not assume one.
	TapAndAwait func(t *testing.T, d driver.Driver)

	// SignInArguments is handed to Driver.LaunchApp on a fresh launch of
	// Hooks.LaunchableAppID to open the demo Sign in screen. A nil value
	// skips the typing check.
	SignInArguments map[string]string

	// TypeAndAwait must prove the keyboard and field contract on the screen
	// SignInArguments opens. It runs once per landscape orientation, with
	// the device already rotated.
	TypeAndAwait func(t *testing.T, d driver.Driver)
}

// TapGestureTarget taps the center of the demo app's tap button and
// fails the test unless the tap counter on the same screen then shows one
// more tap than before. The position comes from the hierarchy read at
// the time of the call, so the tap is only right when the driver's
// gestures and its hierarchy agree on the current orientation.
func TapGestureTarget(t *testing.T, d driver.Driver) {
	t.Helper()
	ctx := context.Background()
	root, before := awaitTapCounter(t, d)
	beforeCount, err := strconv.Atoi(before)
	if err != nil {
		t.Fatalf("tap counter %q is not a number", before)
	}
	center, ok := findNodeCenter(root, tapTargetID)
	if !ok {
		t.Fatalf("no node with id %q and bounds is on the screen", tapTargetID)
	}
	if err := d.Tap(ctx, center); err != nil {
		t.Fatalf("Tap at %+v: %v", center, err)
	}
	want := strconv.Itoa(beforeCount + 1)
	deadline := time.Now().Add(tapCountSettleTimeout)
	for {
		root, err = d.Hierarchy(ctx)
		if err != nil {
			t.Fatalf("Hierarchy while waiting for the tap counter: %v", err)
		}
		if got, ok := findPrefixedText(root, tapCountPrefix); ok && got == want {
			return
		}
		if !time.Now().Before(deadline) {
			got, _ := findPrefixedText(root, tapCountPrefix)
			t.Fatalf("tap at %+v did not land: counter shows %q, want %q", center, got, want)
		}
		time.Sleep(tapCountPoll)
	}
}

// awaitTapCounter reads the hierarchy until it shows the tap counter and
// returns that hierarchy with the counter text. A rotation can recreate the
// app's activity, so the counter may be missing from the first reads.
func awaitTapCounter(t *testing.T, d driver.Driver) (wire.TreeNodeWire, string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(tapCountSettleTimeout)
	for {
		root, err := d.Hierarchy(ctx)
		if err != nil {
			t.Fatalf("Hierarchy before tapping the tap target: %v", err)
		}
		if count, ok := findPrefixedText(root, tapCountPrefix); ok {
			return root, count
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the screen shows no %q counter within %s while the screen is %s",
				strings.TrimSpace(tapCountPrefix), tapCountSettleTimeout, describeScreenLayout(ctx, d))
		}
		time.Sleep(tapCountPoll)
	}
}

// describeScreenLayout names the orientation the screen size implies, for
// failure messages.
func describeScreenLayout(ctx context.Context, d driver.Driver) string {
	size, err := d.ScreenSize(ctx)
	if err != nil {
		return fmt.Sprintf("of unknown size (%v)", err)
	}
	layout := "portrait"
	if size.Width > size.Height {
		layout = "landscape"
	}
	return fmt.Sprintf("%s at %vx%v", layout, size.Width, size.Height)
}

// findPrefixedText returns the text that follows prefix in the label,
// text or value of the first node under root that carries it. The
// returned text ends at the first line break.
func findPrefixedText(root wire.TreeNodeWire, prefix string) (string, bool) {
	for _, key := range []string{"label", "text", "value"} {
		if value, ok := root.Attributes[key]; ok && strings.HasPrefix(value, prefix) {
			rest := strings.TrimPrefix(value, prefix)
			// Some hosts join merged semantics labels with a newline, so the
			// value ends at the first line break.
			if i := strings.IndexByte(rest, '\n'); i >= 0 {
				rest = rest[:i]
			}
			return strings.TrimSpace(rest), true
		}
	}
	for _, child := range root.Children {
		if text, ok := findPrefixedText(child, prefix); ok {
			return text, true
		}
	}
	return "", false
}

// findNodeCenter returns the center of the first node under root whose
// id ends with id and whose bounds have an area. The suffix match
// tolerates platforms that prefix the id with a package name.
func findNodeCenter(root wire.TreeNodeWire, id string) (wire.Point, bool) {
	if strings.HasSuffix(root.Attributes["id"], id) {
		if b, ok := parseNodeBounds(root.Attributes["bounds"]); ok && b.Right > b.Left && b.Bottom > b.Top {
			return wire.Point{X: (b.Left + b.Right) / 2, Y: (b.Top + b.Bottom) / 2}, true
		}
	}
	for _, child := range root.Children {
		if center, ok := findNodeCenter(child, id); ok {
			return center, true
		}
	}
	return wire.Point{}, false
}

// parseNodeBounds decodes the "left,top,right,bottom" form hierarchy
// nodes store their bounds in.
func parseNodeBounds(raw string) (wire.Bounds, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return wire.Bounds{}, false
	}
	values := make([]float64, 4)
	for i, part := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return wire.Bounds{}, false
		}
		values[i] = v
	}
	return wire.Bounds{Left: values[0], Top: values[1], Right: values[2], Bottom: values[3]}, true
}

// testSetOrientationMatchesCapability checks the unsupported-versus-failed
// split. A driver that reports CanSetOrientation false must fail
// SetOrientation with diag.CodeOrientationUnsupported, and one that
// reports it true must reject an unknown orientation with
// diag.CodeInvalidArgument before it touches the device.
func testSetOrientationMatchesCapability(t *testing.T, newDriver Factory) {
	d := newDriver()
	err := d.SetOrientation(context.Background(), "sideways")
	want := diag.CodeInvalidArgument
	if !d.Capabilities().CanSetOrientation {
		want = diag.CodeOrientationUnsupported
	}
	if err == nil {
		t.Fatalf("SetOrientation with an unknown orientation returned no error, want code %q", want)
	}
	if code, _, _ := diag.Describe(err); code != want {
		t.Fatalf("SetOrientation with an unknown orientation failed with code %q (err %v), want %q", code, err, want)
	}
}

// requireScreenLayout fails the test unless ScreenSize reports the
// layout orientation requires.
func requireScreenLayout(t *testing.T, d driver.Driver, orientation driver.Orientation) {
	t.Helper()
	size, err := d.ScreenSize(context.Background())
	if err != nil {
		t.Fatalf("ScreenSize after %s: %v", orientation, err)
	}
	if !driver.SizeMatchesOrientation(orientation, size) {
		t.Fatalf("ScreenSize = %+v after %s, want a screen %s", size, orientation, layoutWord(orientation))
	}
}

func layoutWord(orientation driver.Orientation) string {
	if orientation.IsLandscape() {
		return "wider than tall"
	}
	return "taller than wide"
}

// testSetOrientationReachesTheApp rotates the device into each
// landscape orientation and back, asserting the screen size follows,
// and taps the fixture's target in every orientation to prove that
// gestures land where the hierarchy says. The device is turned back to
// portrait on exit even when the case fails. A driver that reports
// CanSetOrientation false must instead fail the call with
// diag.CodeOrientationUnsupported, which this case asserts rather than
// skips. It skips when the driver is wired without a fixture and can set
// the orientation.
func testSetOrientationReachesTheApp(t *testing.T, newDriver Factory, hooks Hooks) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !d.Capabilities().CanSetOrientation {
		err := d.SetOrientation(ctx, driver.OrientationLandscapeLeft)
		if code, _, _ := diag.Describe(err); code != diag.CodeOrientationUnsupported {
			t.Fatalf("SetOrientation on a driver without the capability failed with code %q (err %v), want %q", code, err, diag.CodeOrientationUnsupported)
		}
		return
	}
	bundleID := requireLaunchable(t, hooks)
	if hooks.Orientation == nil {
		SkipBecause(t, SkipNoOrientationFixture, "no Orientation fixture wired for this driver")
	}
	fixture := hooks.Orientation
	t.Cleanup(func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), orientationRestoreTimeout)
		defer cancel()
		if err := d.SetOrientation(restoreCtx, driver.OrientationPortrait); err != nil {
			t.Errorf("turn the device back to portrait: %v", err)
		}
	})

	fresh := false
	if err := d.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &fresh, Arguments: fixture.Arguments}); err != nil {
		t.Fatalf("LaunchApp: %v", err)
	}
	for _, orientation := range []driver.Orientation{
		driver.OrientationPortrait,
		driver.OrientationLandscapeLeft,
		driver.OrientationLandscapeRight,
		driver.OrientationPortrait,
	} {
		if err := d.SetOrientation(ctx, orientation); err != nil {
			t.Fatalf("SetOrientation %s: %v", orientation, err)
		}
		requireScreenLayout(t, d, orientation)
		time.Sleep(fixture.SettlePause)
		fixture.TapAndAwait(t, d)
	}
}

// testKeyboardAndFieldsAgreeInLandscape opens the Sign in screen, turns the
// device into each landscape orientation, and runs the fixture's check
// there. The device is turned back to portrait on exit even when the case
// fails. It skips when the driver cannot set the orientation or is wired
// without a sign-in fixture.
func testKeyboardAndFieldsAgreeInLandscape(t *testing.T, newDriver Factory, hooks Hooks) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !d.Capabilities().CanSetOrientation {
		SkipBecause(t, SkipNoOrientationFixture, "the driver cannot set the orientation")
	}
	bundleID := requireLaunchable(t, hooks)
	fixture := hooks.Orientation
	if fixture == nil || fixture.SignInArguments == nil || fixture.TypeAndAwait == nil {
		SkipBecause(t, SkipNoOrientationFixture, "no sign-in typing fixture wired for this driver")
	}
	t.Cleanup(func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), orientationRestoreTimeout)
		defer cancel()
		if err := d.SetOrientation(restoreCtx, driver.OrientationPortrait); err != nil {
			t.Errorf("turn the device back to portrait: %v", err)
		}
	})

	fresh := false
	if err := d.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &fresh, Arguments: fixture.SignInArguments}); err != nil {
		t.Fatalf("LaunchApp: %v", err)
	}
	for _, orientation := range []driver.Orientation{driver.OrientationLandscapeLeft, driver.OrientationLandscapeRight} {
		if err := d.SetOrientation(ctx, orientation); err != nil {
			t.Fatalf("SetOrientation %s: %v", orientation, err)
		}
		requireScreenLayout(t, d, orientation)
		time.Sleep(fixture.SettlePause)
		fixture.TypeAndAwait(t, d)
	}
}
