package drivertest

import (
	"context"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

const (
	// signInEmailLabel and signInPasswordLabel are the labels of the demo
	// app's Sign in fields.
	signInEmailLabel    = "Email"
	signInPasswordLabel = "Password"

	// signInEmail is the value the case types into the Email field.
	signInEmail = "qa@example.com"

	// signInSettleTimeout bounds how long the case waits for the keyboard
	// or a typed value to show in the hierarchy.
	signInSettleTimeout = 10 * time.Second

	// signInSettlePoll is the gap between hierarchy reads while waiting.
	signInSettlePoll = 250 * time.Millisecond

	// keyboardEdgeTolerance is how far, in screen units, the keyboard's
	// bottom edge may sit from the bottom of the screen.
	keyboardEdgeTolerance = 2.0
)

// VerifySignInKeyboard checks what the driver owes a caller that must
// avoid the on-screen keyboard. It taps the demo app's Email field at
// the center the hierarchy reports, then fails the test unless:
//
//   - the keyboard appears in the hierarchy with bounds that fit inside
//     ScreenSize and touch its bottom edge
//   - the Email and Password bounds read from that same hierarchy lie
//     inside the screen, so keyboard and fields share one coordinate space
//   - text typed through Driver.InputText shows in the Email field
func VerifySignInKeyboard(t *testing.T, d driver.Driver) {
	t.Helper()
	ctx := context.Background()
	root, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy before tapping the %s field: %v", signInEmailLabel, err)
	}
	email, ok := findLabelBounds(root, signInEmailLabel)
	if !ok {
		t.Fatalf("no %s field with bounds is on the screen", signInEmailLabel)
	}
	center := wire.Point{X: (email.Left + email.Right) / 2, Y: (email.Top + email.Bottom) / 2}
	if err := d.Tap(ctx, center); err != nil {
		t.Fatalf("Tap at %+v: %v", center, err)
	}

	keyboard, root := awaitKeyboardBounds(t, d)
	screen, err := d.ScreenSize(ctx)
	if err != nil {
		t.Fatalf("ScreenSize: %v", err)
	}
	if !boundsFit(keyboard, screen) {
		t.Fatalf("keyboard bounds %+v do not fit inside the %vx%v screen", keyboard, screen.Width, screen.Height)
	}
	if keyboard.Bottom < screen.Height-keyboardEdgeTolerance {
		t.Fatalf("keyboard bottom %v does not touch the screen bottom %v", keyboard.Bottom, screen.Height)
	}
	for _, label := range []string{signInEmailLabel, signInPasswordLabel} {
		b, ok := findLabelBounds(root, label)
		if !ok {
			t.Fatalf("no %s field with bounds is in the hierarchy that shows the keyboard", label)
		}
		if !boundsFit(b, screen) {
			t.Fatalf("%s bounds %+v are outside the %vx%v screen, so fields and keyboard disagree on coordinates", label, b, screen.Width, screen.Height)
		}
	}

	if err := d.InputText(ctx, signInEmail); err != nil {
		t.Fatalf("InputText into the %s field: %v", signInEmailLabel, err)
	}
	awaitFieldValue(t, d, signInEmailLabel, signInEmail)
}

// awaitKeyboardBounds reads the hierarchy until it holds a keyboard node
// whose bounds fit inside the screen, and returns those bounds with the
// hierarchy they came from. When the deadline passes, it fails with the last
// keyboard bounds it saw and the screen size.
func awaitKeyboardBounds(t *testing.T, d driver.Driver) (wire.Bounds, wire.TreeNodeWire) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(signInSettleTimeout)
	var last wire.Bounds
	seen := false
	for {
		root, err := d.Hierarchy(ctx)
		if err != nil {
			t.Fatalf("Hierarchy while waiting for the keyboard: %v", err)
		}
		screen, err := d.ScreenSize(ctx)
		if err != nil {
			t.Fatalf("ScreenSize while waiting for the keyboard: %v", err)
		}
		if b, ok := findKeyboardBounds(root); ok {
			last, seen = b, true
			if boundsFit(b, screen) {
				return b, root
			}
		}
		if !time.Now().Before(deadline) {
			if !seen {
				t.Fatalf("no keyboard with bounds appeared within %s", signInSettleTimeout)
			}
			t.Fatalf("keyboard bounds %+v still do not fit inside the %vx%v screen after %s", last, screen.Width, screen.Height, signInSettleTimeout)
		}
		time.Sleep(signInSettlePoll)
	}
}

// findKeyboardBounds returns the bounds of the first keyboard node under
// root. A node is a keyboard when its role is keyboard or it is marked as
// an input method window.
func findKeyboardBounds(root wire.TreeNodeWire) (wire.Bounds, bool) {
	if root.Attributes["role"] == "keyboard" || root.Attributes["ext:isIme"] == "true" {
		if b, ok := parseNodeBounds(root.Attributes["bounds"]); ok {
			return b, true
		}
	}
	for _, child := range root.Children {
		if b, ok := findKeyboardBounds(child); ok {
			return b, true
		}
	}
	return wire.Bounds{}, false
}

// findLabelBounds returns the bounds of the first node under root named
// by label, with an area.
func findLabelBounds(root wire.TreeNodeWire, label string) (wire.Bounds, bool) {
	if nodeNamed(root, label) {
		if b, ok := parseNodeBounds(root.Attributes["bounds"]); ok && b.Right > b.Left && b.Bottom > b.Top {
			return b, true
		}
	}
	for _, child := range root.Children {
		if b, ok := findLabelBounds(child, label); ok {
			return b, true
		}
	}
	return wire.Bounds{}, false
}

// nodeNamed reports whether any naming attribute of n starts with label.
func nodeNamed(n wire.TreeNodeWire, label string) bool {
	for _, key := range []string{"label", "text", "hint", "value"} {
		if strings.HasPrefix(n.Attributes[key], label) {
			return true
		}
	}
	return false
}

// boundsFit reports whether b lies inside a screen of the given size.
func boundsFit(b wire.Bounds, screen wire.Size) bool {
	return b.Left >= 0 && b.Top >= 0 && b.Right <= screen.Width && b.Bottom <= screen.Height
}

// awaitFieldValue reads the hierarchy until the field named label shows
// value, and fails the test when it does not within the bound.
func awaitFieldValue(t *testing.T, d driver.Driver, label, value string) {
	t.Helper()
	deadline := time.Now().Add(signInSettleTimeout)
	for {
		root, err := d.Hierarchy(context.Background())
		if err != nil {
			t.Fatalf("Hierarchy while waiting for the %s field value: %v", label, err)
		}
		if fieldShows(root, label, value) {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the %s field does not show %q within %s", label, value, signInSettleTimeout)
		}
		time.Sleep(signInSettlePoll)
	}
}

// fieldShows reports whether some node under root is named by label and
// carries value in its value, text or label.
func fieldShows(root wire.TreeNodeWire, label, value string) bool {
	if nodeNamed(root, label) {
		for _, key := range []string{"value", "text", "label"} {
			if strings.Contains(root.Attributes[key], value) {
				return true
			}
		}
	}
	for _, child := range root.Children {
		if fieldShows(child, label, value) {
			return true
		}
	}
	return false
}
