package drivertest

import (
	"context"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

// unsupportedPermissionsAppID names the app the unsupported-device check
// asks about when no launchable app is wired, because that check never
// reaches an app.
const unsupportedPermissionsAppID = "dev.atomyx.drivertest.permissions"

const (
	// permissionStatusTimeout bounds how long AwaitPermissionStatus waits
	// for the permission screen to report a settled status.
	permissionStatusTimeout = 15 * time.Second

	// permissionStatusPoll is the gap between hierarchy reads while
	// waiting.
	permissionStatusPoll = 250 * time.Millisecond

	// permissionStatusPrefix starts the label the demo app's permission
	// screen shows for the location permission.
	permissionStatusPrefix = "Location status: "

	// permissionStatusPending is the status the screen shows before it
	// has read the permission.
	permissionStatusPending = "unknown"

	// permissionStatusGranted is the status the screen shows for a
	// granted permission.
	permissionStatusGranted = "granted"
)

// PermissionsFixture describes an app screen that shows the state of one
// permission, so a case can prove a grant or a revoke reached the app and
// not only the driver.
type PermissionsFixture struct {
	// Permission is the permission the screen shows.
	Permission driver.Permission

	// Arguments is handed to Driver.LaunchApp on a fresh launch of
	// Hooks.LaunchableAppID to open the screen.
	Arguments map[string]string

	// Await must block until the screen shows the permission as granted
	// or not granted, as wantGranted says, and fail the test when it
	// does not. It observes the screen through d.
	Await func(t *testing.T, d driver.Driver, wantGranted bool)
}

// AwaitPermissionStatus reads the hierarchy until the demo app's
// permission screen reports a settled status, and fails the test unless
// that status is granted when wantGranted is true, or anything but
// granted when it is false. The screen first shows "unknown" while it
// reads the permission, and that status never counts as settled.
func AwaitPermissionStatus(t *testing.T, d driver.Driver, wantGranted bool) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(permissionStatusTimeout)
	for {
		root, err := d.Hierarchy(ctx)
		if err != nil {
			t.Fatalf("Hierarchy while waiting for the permission status: %v", err)
		}
		if status, ok := findPermissionStatus(root); ok && status != permissionStatusPending {
			if (status == permissionStatusGranted) != wantGranted {
				t.Fatalf("permission status = %q, want granted = %v", status, wantGranted)
			}
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the permission screen showed no settled status within %s", permissionStatusTimeout)
		}
		time.Sleep(permissionStatusPoll)
	}
}

// findPermissionStatus returns the status text of the first node under
// root whose label or text carries the permission status prefix.
func findPermissionStatus(root wire.TreeNodeWire) (string, bool) {
	for _, key := range []string{"label", "text", "value"} {
		if value, ok := root.Attributes[key]; ok && strings.HasPrefix(value, permissionStatusPrefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, permissionStatusPrefix)), true
		}
	}
	for _, child := range root.Children {
		if status, ok := findPermissionStatus(child); ok {
			return status, true
		}
	}
	return "", false
}

// testSetPermissionsMatchesCapability checks the unsupported-versus-failed
// split. A driver that reports CanSetPermissions false must fail
// SetPermissions with diag.CodePermissionsUnsupported, and one that
// reports it true must reject an unknown permission name with
// diag.CodeInvalidArgument before it touches the device.
func testSetPermissionsMatchesCapability(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	err := d.SetPermissions(ctx, "dev.atomyx.drivertest.permissions",
		map[driver.Permission]driver.PermissionState{"not-a-permission": driver.PermissionAllow})
	want := diag.CodeInvalidArgument
	if !d.Capabilities().CanSetPermissions {
		want = diag.CodePermissionsUnsupported
	}
	if err == nil {
		t.Fatalf("SetPermissions with an unknown permission returned no error, want code %q", want)
	}
	if code, _, _ := diag.Describe(err); code != want {
		t.Fatalf("SetPermissions with an unknown permission failed with code %q (err %v), want %q", code, err, want)
	}
}

// testSetPermissionsReachesTheApp grants the fixture permission, starts
// the app on its permission screen and waits for it to show the grant,
// then revokes the permission and checks the screen again. A driver that
// reports CanSetPermissions false must instead fail the call with
// diag.CodePermissionsUnsupported, which this case asserts rather than
// skips. It skips when the driver is wired without a fixture and can
// set permissions.
func testSetPermissionsReachesTheApp(t *testing.T, newDriver Factory, hooks Hooks) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !d.Capabilities().CanSetPermissions {
		bundleID := hooks.LaunchableAppID
		if bundleID == "" {
			bundleID = unsupportedPermissionsAppID
		}
		err := d.SetPermissions(ctx, bundleID, map[driver.Permission]driver.PermissionState{driver.PermissionLocation: driver.PermissionAllow})
		if code, _, _ := diag.Describe(err); code != diag.CodePermissionsUnsupported {
			t.Fatalf("SetPermissions on a driver without the capability failed with code %q (err %v), want %q", code, err, diag.CodePermissionsUnsupported)
		}
		return
	}
	bundleID := requireLaunchable(t, hooks)
	if hooks.Permissions == nil {
		SkipBecause(t, SkipNoPermissionsFixture, "no Permissions fixture wired for this driver")
	}
	fixture := hooks.Permissions
	fresh := false
	for _, step := range []struct {
		state       driver.PermissionState
		wantGranted bool
	}{
		{driver.PermissionAllow, true},
		{driver.PermissionDeny, false},
	} {
		if err := d.SetPermissions(ctx, bundleID, map[driver.Permission]driver.PermissionState{fixture.Permission: step.state}); err != nil {
			t.Fatalf("SetPermissions %s %s: %v", step.state, fixture.Permission, err)
		}
		if err := d.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &fresh, Arguments: fixture.Arguments}); err != nil {
			t.Fatalf("LaunchApp after %s %s: %v", step.state, fixture.Permission, err)
		}
		fixture.Await(t, d, step.wantGranted)
	}
}
