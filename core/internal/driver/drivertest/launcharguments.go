package drivertest

import (
	"context"
	"testing"

	"dev.atomyx/internal/driver"
)

// LaunchArgumentsFixture describes an app that visibly reacts to launch
// arguments, so a case can prove the values reached the app and not only
// the driver.
type LaunchArgumentsFixture struct {
	// Arguments is handed to Driver.LaunchApp on a fresh launch of
	// Hooks.LaunchableAppID.
	Arguments map[string]string

	// Await must block until the app shows the effect of Arguments and
	// fail the test when it does not. It observes the screen through d.
	Await func(t *testing.T, d driver.Driver)
}

// testLaunchAppDeliversArguments launches the launchable app fresh with
// the fixture's arguments and waits for the app to show their effect.
// It skips when the driver is wired without an arguments fixture.
func testLaunchAppDeliversArguments(t *testing.T, newDriver Factory, hooks Hooks) {
	bundleID := requireLaunchable(t, hooks)
	if hooks.LaunchArguments == nil {
		SkipBecause(t, SkipNoLaunchArgumentsFixture, "no LaunchArguments fixture wired for this driver")
	}
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	fresh := false
	args := driver.LaunchArgs{NoReset: &fresh, Arguments: hooks.LaunchArguments.Arguments}
	if err := d.LaunchApp(ctx, bundleID, args); err != nil {
		t.Fatalf("LaunchApp with arguments: %v", err)
	}
	hooks.LaunchArguments.Await(t, d)
}
