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
	// focusSettleTimeout bounds how long AwaitFocusedInput waits for the
	// field to take focus after the screen that hosts it opens.
	focusSettleTimeout = 15 * time.Second

	// focusSettlePoll is the gap between hierarchy reads while waiting.
	focusSettlePoll = 250 * time.Millisecond
)

// AwaitFocusedInput reads the hierarchy until a node whose id ends with
// inputID reports focus, and fails the test when none does within the
// bound. It observes the screen through the driver under test, so a
// FocusInput hook can open a route by any host-side means and then
// prove the field is ready for erase-text.
func AwaitFocusedInput(t *testing.T, d driver.Driver, inputID string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(focusSettleTimeout)
	for {
		root, err := d.Hierarchy(ctx)
		if err != nil {
			t.Fatalf("Hierarchy while waiting for %q to take focus: %v", inputID, err)
		}
		if hasFocusedInput(root, inputID) {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no node with id %q reported focus within %s", inputID, focusSettleTimeout)
		}
		time.Sleep(focusSettlePoll)
	}
}

// hasFocusedInput reports whether any node under root has an id ending
// with inputID and an explicit focused state of true. The suffix match
// tolerates platforms that prefix the id with a package name.
func hasFocusedInput(root wire.TreeNodeWire, inputID string) bool {
	if root.Focused != nil && *root.Focused && strings.HasSuffix(root.Attributes["id"], inputID) {
		return true
	}
	for _, child := range root.Children {
		if hasFocusedInput(child, inputID) {
			return true
		}
	}
	return false
}
