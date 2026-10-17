package drivertest

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
)

const (
	// signInEmailLabel and signInPasswordLabel are the labels of the demo
	// app's Sign in fields.
	signInEmailLabel    = "Email"
	signInPasswordLabel = "Password"

	// signInEmail and signInPassword are the values the case types.
	signInEmail    = "qa@example.com"
	signInPassword = "s3cret-pass"

	// signInSettleTimeout bounds how long the case waits for a typed
	// value to show in the hierarchy.
	signInSettleTimeout = 10 * time.Second

	// signInSettlePoll is the gap between hierarchy reads while waiting.
	signInSettlePoll = 250 * time.Millisecond
)

// signInFields lists the fields the case fills, in typing order.
var signInFields = []struct {
	label, value string
	masked       bool
}{
	{signInEmailLabel, signInEmail, false},
	{signInPasswordLabel, signInPassword, true},
}

// TypeIntoSignInFields types into the demo app's Email field and then
// its Password field, both addressed by label, and fails the test unless
// each field then shows its value. The Password field may show a masked
// value, so it only has to show as many characters as were typed.
//
// The Password field is the one the on-screen keyboard covers in a
// landscape layout, so the case proves that a type action reaches a
// field the keyboard from the previous field would otherwise hide.
func TypeIntoSignInFields(t *testing.T, d driver.Driver) {
	t.Helper()
	ctx := context.Background()
	o := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: orchestra.SystemClock{}})
	for _, field := range signInFields {
		label := orchestra.LiteralPattern(field.label)
		result, err := o.InputText(ctx, orchestra.Selector{Text: &label}, field.value, orchestra.InputTextOptions{})
		if err != nil {
			t.Fatalf("type into the %s field: %v", field.label, err)
		}
		if !result.Ok {
			t.Fatalf("type into the %s field was rejected: %s", field.label, result.Reason)
		}
	}
	for _, field := range signInFields {
		awaitFieldValue(t, d, field.label, field.value, field.masked)
	}
}

// awaitFieldValue reads the hierarchy until the field named label shows
// value, and fails the test when it does not within the bound.
func awaitFieldValue(t *testing.T, d driver.Driver, label, value string, masked bool) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(signInSettleTimeout)
	for {
		root, err := d.Hierarchy(ctx)
		if err != nil {
			t.Fatalf("Hierarchy while waiting for the %s field value: %v", label, err)
		}
		if fieldShows(root, label, value, masked) {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the %s field does not show %q within %s", label, value, signInSettleTimeout)
		}
		time.Sleep(signInSettlePoll)
	}
}

// fieldShows reports whether some node under root is named by label and
// carries value in its value, text or label. A masked field is satisfied
// by a value of the same length instead, because secure fields replace
// the characters.
func fieldShows(root wire.TreeNodeWire, label, value string, masked bool) bool {
	attrs := root.Attributes
	named := false
	for _, key := range []string{"label", "text", "hint", "value"} {
		if strings.HasPrefix(attrs[key], label) {
			named = true
		}
	}
	if named {
		for _, key := range []string{"value", "text", "label"} {
			got := attrs[key]
			if strings.Contains(got, value) {
				return true
			}
			if masked && key == "value" && utf8.RuneCountInString(got) == utf8.RuneCountInString(value) {
				return true
			}
		}
	}
	for _, child := range root.Children {
		if fieldShows(child, label, value, masked) {
			return true
		}
	}
	return false
}
