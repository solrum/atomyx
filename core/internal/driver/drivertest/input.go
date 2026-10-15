package drivertest

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

func testTapLongPressSwipeSucceed(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := d.Tap(ctx, wire.Point{X: 1, Y: 1}); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if err := d.LongPress(ctx, wire.Point{X: 1, Y: 1}, 100); err != nil {
		t.Fatalf("LongPress: %v", err)
	}
	if err := d.Swipe(ctx, wire.Point{X: 1, Y: 1}, wire.Point{X: 2, Y: 2}, 100); err != nil {
		t.Fatalf("Swipe: %v", err)
	}
}

// testDispatchGestureMultiPointerCapability asserts the rejection
// Driver.DispatchGesture documents: a gesture carrying more than one
// pointer must error when Capabilities().CanMultiPointer is false,
// and must be accepted when it is true.
func testDispatchGestureMultiPointerCapability(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "a", Waypoints: []driver.GestureWaypoint{{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 1}}}},
		{ID: "b", Waypoints: []driver.GestureWaypoint{{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 2, Y: 2}}}},
	}}

	_, err := d.DispatchGesture(ctx, gesture)
	if !d.Capabilities().CanMultiPointer {
		if err == nil {
			t.Fatal("DispatchGesture with two pointers succeeded on a driver reporting CanMultiPointer false")
		}
		return
	}
	if err != nil {
		t.Fatalf("DispatchGesture with two pointers on a driver reporting CanMultiPointer true: %v", err)
	}
}

// testDispatchGesturePressureCapability asserts the rejection
// Driver.DispatchGesture documents: a gesture waypoint carrying a
// pressure value must error when Capabilities().CanPressure is
// false, and must be accepted when it is true.
func testDispatchGesturePressureCapability(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	pressure := 0.5
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "a", Waypoints: []driver.GestureWaypoint{{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 1}, Pressure: &pressure}}},
	}}

	_, err := d.DispatchGesture(ctx, gesture)
	if !d.Capabilities().CanPressure {
		if err == nil {
			t.Fatal("DispatchGesture with a pressure waypoint succeeded on a driver reporting CanPressure false")
		}
		return
	}
	if err != nil {
		t.Fatalf("DispatchGesture with a pressure waypoint on a driver reporting CanPressure true: %v", err)
	}
}

// testDispatchGestureCaptureCapability asserts the contract every
// driver honors for a mid-gesture capture waypoint: either it
// dispatches successfully and returns exactly one non-empty captured
// image per capture marker, or it rejects the gesture with the typed
// diag.CodePointerCaptureUnsupported error. A driver is free to pick
// either behavior, but never a plain unrelated error and never a
// silent success with no captured image.
func testDispatchGestureCaptureCapability(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "single", Waypoints: []driver.GestureWaypoint{
			{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 1}},
			{Phase: driver.GesturePhaseCapture, Point: wire.Point{X: 1, Y: 1}},
			{Phase: driver.GesturePhaseUp, Point: wire.Point{X: 1, Y: 1}},
		}},
	}}

	result, err := d.DispatchGesture(ctx, gesture)
	if err != nil {
		if code, _, _ := diag.Describe(err); code != diag.CodePointerCaptureUnsupported {
			t.Fatalf("DispatchGesture with a capture marker failed with code %q, want %q", code, diag.CodePointerCaptureUnsupported)
		}
		return
	}
	if len(result.CapturedImages) != 1 {
		t.Fatalf("DispatchGesture with one capture marker returned %d captured images, want 1", len(result.CapturedImages))
	}
	if len(result.CapturedImages[0].Data) == 0 {
		t.Fatal("DispatchGesture returned a capture with empty image data")
	}
}

// testEraseTextCapability asserts the fallback contract
// Driver.EraseText documents: a caller must only invoke it when
// Capabilities().CanEraseText is true. A driver reporting the flag
// false must reject the call rather than silently perform it, so a
// caller that skips the check still fails safely instead of getting
// an operation the driver never actually promised. The positive
// assertion needs a focused input, so it runs only when
// hooks.FocusInput is wired.
func testEraseTextCapability(t *testing.T, newDriver Factory, hooks Hooks) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !d.Capabilities().CanEraseText {
		if err := d.EraseText(ctx, 1); err == nil {
			t.Fatal("EraseText succeeded on a driver reporting CanEraseText false")
		}
		return
	}
	if hooks.FocusInput == nil {
		SkipBecause(t, SkipNoFocusInput, "no FocusInput hook wired for this driver")
	}
	hooks.FocusInput(t, d)
	if err := d.EraseText(ctx, 1); err != nil {
		t.Fatalf("EraseText on a driver reporting CanEraseText true: %v", err)
	}
}

// launchWhenWired launches hooks.LaunchableAppID when it is set, so a
// driver that tracks the launched app internally has a target for
// keyboard and key-press calls. It does nothing when the hook is
// empty.
func launchWhenWired(t *testing.T, d driver.Driver, hooks Hooks) {
	t.Helper()
	if hooks.LaunchableAppID == "" {
		return
	}
	if err := d.LaunchApp(context.Background(), hooks.LaunchableAppID, driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp(%q): %v", hooks.LaunchableAppID, err)
	}
}

// testHideKeyboardCapability asserts the same caller-must-check
// contract as testEraseTextCapability, for
// Capabilities().CanHideKeyboard. It launches the launchable app
// first when that hook is set.
func testHideKeyboardCapability(t *testing.T, newDriver Factory, hooks Hooks) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	launchWhenWired(t, d, hooks)
	_, err := d.HideKeyboard(ctx)
	if !d.Capabilities().CanHideKeyboard {
		if err == nil {
			t.Fatal("HideKeyboard succeeded on a driver reporting CanHideKeyboard false")
		}
		return
	}
	if err != nil {
		t.Fatalf("HideKeyboard on a driver reporting CanHideKeyboard true: %v", err)
	}
}

// testPressKeySupportedCode asserts that pressing the first key code
// a driver advertises in Capabilities().SupportedKeyCodes succeeds,
// launching the launchable app first when that hook is set.
func testPressKeySupportedCode(t *testing.T, newDriver Factory, hooks Hooks) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	launchWhenWired(t, d, hooks)
	codes := d.Capabilities().SupportedKeyCodes
	if len(codes) == 0 {
		SkipBecause(t, SkipNoKeyCodes, "driver reports no supported key codes")
	}
	if _, err := d.PressKey(ctx, codes[0]); err != nil {
		t.Fatalf("PressKey(%q): %v", codes[0], err)
	}
}

// testPressKeyUnsupportedCodeHandledGracefully asserts the KeyCode
// contract's driver-specific-key allowance: a value outside the
// named constants may be accepted (treated as a driver-specific key)
// or rejected, at the driver's discretion, so this only asserts the
// call returns instead of panicking.
func testPressKeyUnsupportedCodeHandledGracefully(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	const unsupported driver.KeyCode = "drivertest.unsupported-key"
	_, _ = d.PressKey(ctx, unsupported)
}

// testStreamingTouchOptional asserts driver.StreamingTouchCapable's
// contract when a subject implements it, and skips cleanly when it
// does not: the interface is an optional capability a caller detects
// via driver.AsStreamingTouchCapable, not a required Driver method.
func testStreamingTouchOptional(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	streaming, ok := driver.AsStreamingTouchCapable(d)
	if !ok {
		SkipBecause(t, SkipNoStreamingTouch, "driver does not implement StreamingTouchCapable")
	}
	if err := streaming.StreamingTouchDown(ctx, wire.Point{X: 1, Y: 1}, 1); err != nil {
		t.Fatalf("StreamingTouchDown: %v", err)
	}
	if err := streaming.StreamingTouchMove(ctx, wire.Point{X: 2, Y: 2}, 1); err != nil {
		t.Fatalf("StreamingTouchMove: %v", err)
	}
	if err := streaming.StreamingTouchUp(ctx, wire.Point{X: 2, Y: 2}, 1); err != nil {
		t.Fatalf("StreamingTouchUp: %v", err)
	}
}

// testForeignAutomationOptional asserts driver.ForeignAutomationReporter's
// contract when a subject implements it, and skips cleanly when it
// does not: the interface is an optional capability a caller detects
// via driver.As, not a required Driver method. A report must be stable
// between calls, name every session, and be safe for the caller to
// modify.
func testForeignAutomationOptional(t *testing.T, newDriver Factory) {
	d := newDriver()
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	reporter, ok := driver.AsForeignAutomationReporter(d)
	if !ok {
		SkipBecause(t, SkipNoForeignAutomation, "driver does not implement ForeignAutomationReporter")
	}
	first := reporter.ForeignAutomation()
	for i, s := range first {
		if s.Name == "" || s.Kind == "" {
			t.Fatalf("session %d has an empty field: %+v", i, s)
		}
	}
	if len(first) > 0 {
		first[0].Name = "mutated"
	}
	second := reporter.ForeignAutomation()
	if len(second) > 0 && second[0].Name == "mutated" {
		t.Fatal("ForeignAutomation returned a slice that aliases its cache")
	}
}
