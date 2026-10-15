package drivertest

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// assertForeground checks a CurrentForeground result against d's
// CanQueryForeground capability, so no adapter can either lie with a
// fabricated value or claim support while erroring:
//
//   - When CanQueryForeground is true, err must be nil and
//     foreground's bundle id must match wantBundleID (empty meaning
//     no app in the foreground).
//   - When CanQueryForeground is false, err must carry the typed
//     unsupported code and foreground must carry no bundle id.
//
// label identifies the calling point in a failure message, for
// example "after launch".
func assertForeground(t *testing.T, d driver.Driver, foreground driver.ForegroundInfo, err error, wantBundleID, label string) {
	t.Helper()
	if d.Capabilities().CanQueryForeground {
		if err != nil {
			t.Fatalf("CurrentForeground %s: %v", label, err)
		}
		switch {
		case wantBundleID == "" && foreground.BundleID != nil:
			t.Fatalf("CurrentForeground %s reported %q, want none", label, *foreground.BundleID)
		case wantBundleID != "" && (foreground.BundleID == nil || *foreground.BundleID != wantBundleID):
			t.Fatalf("CurrentForeground %s = %+v, want bundleId %q", label, foreground, wantBundleID)
		}
		return
	}
	if err == nil {
		t.Fatalf("CurrentForeground %s succeeded with %+v, want the unsupported error since CanQueryForeground is false", label, foreground)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeForegroundUnsupported {
		t.Fatalf("CurrentForeground %s error code = %q, want %q", label, code, diag.CodeForegroundUnsupported)
	}
	if foreground.BundleID != nil {
		t.Fatalf("CurrentForeground %s returned bundleId %q alongside an error, want none", label, *foreground.BundleID)
	}
}

// assertNotForeground checks that bundleID is not in front. Unlike
// assertForeground with an empty want, it tolerates a launcher or any
// other app in front, which is what a real device shows after an app
// stops. A driver without foreground support is checked through
// assertForeground so it still must return the typed unsupported error.
func assertNotForeground(t *testing.T, d driver.Driver, foreground driver.ForegroundInfo, err error, bundleID, label string) {
	t.Helper()
	if !d.Capabilities().CanQueryForeground {
		assertForeground(t, d, foreground, err, "", label)
		return
	}
	if err != nil {
		t.Fatalf("CurrentForeground %s: %v", label, err)
	}
	if foreground.BundleID != nil && *foreground.BundleID == bundleID {
		t.Fatalf("CurrentForeground %s still reports %q", label, bundleID)
	}
}

func requireLaunchable(t *testing.T, hooks Hooks) string {
	t.Helper()
	if hooks.LaunchableAppID == "" {
		t.Skip("drivertest: no LaunchableAppID wired for this driver; skipping the launch case")
	}
	return hooks.LaunchableAppID
}

func testLaunchAppLifecycle(t *testing.T, newDriver Factory, hooks Hooks) {
	bundleID := requireLaunchable(t, hooks)
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if hooks.NoForeground != nil {
		hooks.NoForeground(t, d)
		foreground, err := d.CurrentForeground(ctx)
		assertForeground(t, d, foreground, err, "", "before launch")
	}

	if err := d.LaunchApp(ctx, bundleID, driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp: %v", err)
	}
	foreground, err := awaitForeground(ctx, d, bundleID, true)
	assertForeground(t, d, foreground, err, bundleID, "after launch")

	if err := d.StopApp(ctx, bundleID); err != nil {
		t.Fatalf("StopApp: %v", err)
	}
	foreground, err = awaitForeground(ctx, d, bundleID, false)
	if hooks.NoForeground != nil {
		assertForeground(t, d, foreground, err, "", "after stop")
		return
	}
	assertNotForeground(t, d, foreground, err, bundleID, "after stop")
}

// testLaunchAppFreshStopsThenLaunches exercises the NoReset=false
// branch of LaunchApp's tri-state, the one testLaunchAppLifecycle
// (plain) and testLaunchAppAttach (NoReset=true) do not cover: a
// fresh launch of an already-running app stops it first, then
// launches a clean process that still ends up foreground.
func testLaunchAppFreshStopsThenLaunches(t *testing.T, newDriver Factory, hooks Hooks) {
	bundleID := requireLaunchable(t, hooks)
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := d.LaunchApp(ctx, bundleID, driver.LaunchArgs{}); err != nil {
		t.Fatalf("initial LaunchApp: %v", err)
	}
	fresh := false
	if err := d.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &fresh}); err != nil {
		t.Fatalf("fresh LaunchApp: %v", err)
	}
	foreground, err := awaitForeground(ctx, d, bundleID, true)
	assertForeground(t, d, foreground, err, bundleID, "after a fresh launch")
}

func testLaunchAppAttach(t *testing.T, newDriver Factory, hooks Hooks) {
	bundleID := requireLaunchable(t, hooks)
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	attach := true

	if err := d.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &attach}); err != nil {
		t.Fatalf("attach launch on a cold app: %v", err)
	}
	foreground, err := awaitForeground(ctx, d, bundleID, true)
	assertForeground(t, d, foreground, err, bundleID, "after an attach launch on a cold app")

	if err := d.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &attach}); err != nil {
		t.Fatalf("attach launch while already foreground: %v", err)
	}
	foreground, err = awaitForeground(ctx, d, bundleID, true)
	assertForeground(t, d, foreground, err, bundleID, "after an attach launch while already foreground")
}

// testClearAppDataSucceedsWithSource calls ClearAppData with a
// non-empty Source, the one input every adapter accepts: Android
// ignores Source and clears in place, iOS uninstalls and reinstalls
// from it (see Driver.ClearAppData's platform notes). Source comes
// from hooks.Install when wired and is a placeholder otherwise, so a
// caller whose adapter reinstalls from it must wire Install. Source's
// omitted behavior is genuinely platform-specific. iOS has no way to
// reach a clean state without it, so it errors, while Android does
// not need it. This case is covered by each adapter's own test instead of
// this shared suite.
func testClearAppDataSucceedsWithSource(t *testing.T, newDriver Factory, hooks Hooks) {
	if hooks.ClearableAppID == "" {
		t.Skip("drivertest: no ClearableAppID wired for this driver; skipping the clear app data case")
	}
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	source := "/drivertest/placeholder"
	if hooks.Install != nil {
		source = hooks.Install.Source
	}
	if err := d.ClearAppData(ctx, hooks.ClearableAppID, driver.ClearAppDataOptions{Source: source}); err != nil {
		t.Fatalf("ClearAppData with Source set: %v", err)
	}
}

func testInstallUninstallRoundtrip(t *testing.T, newDriver Factory, hooks Hooks) {
	if hooks.Install == nil {
		t.Skip("drivertest: no Install fixture wired for this driver; skipping the install and uninstall roundtrip")
	}
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	appID := hooks.Install.AppID

	installed, err := d.IsAppInstalled(ctx, appID)
	if err != nil {
		t.Fatalf("IsAppInstalled before install: %v", err)
	}
	if installed {
		if err := d.UninstallApp(ctx, appID); err != nil {
			t.Fatalf("UninstallApp of the already-installed fixture: %v", err)
		}
		installed, err = d.IsAppInstalled(ctx, appID)
		if err != nil {
			t.Fatalf("IsAppInstalled after clearing the fixture: %v", err)
		}
		if installed {
			t.Fatal("IsAppInstalled after clearing the fixture reported true")
		}
	}

	if err := d.InstallApp(ctx, hooks.Install.Source); err != nil {
		t.Fatalf("InstallApp: %v", err)
	}
	installed, err = d.IsAppInstalled(ctx, appID)
	if err != nil {
		t.Fatalf("IsAppInstalled after install: %v", err)
	}
	if !installed {
		t.Fatal("IsAppInstalled after install reported false")
	}

	if err := d.UninstallApp(ctx, appID); err != nil {
		t.Fatalf("UninstallApp: %v", err)
	}
	installed, err = d.IsAppInstalled(ctx, appID)
	if err != nil {
		t.Fatalf("IsAppInstalled after uninstall: %v", err)
	}
	if installed {
		t.Fatal("IsAppInstalled after uninstall reported true")
	}
}
