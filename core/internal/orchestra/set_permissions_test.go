package orchestra

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
)

func permissionCalls(d *mock.Driver) []mock.PermissionChange { return d.PermissionChanges }

func TestSetPermissions_PassesTheAppAndPermissionsToTheDriver(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)

	err := o.SetPermissions(context.Background(), "com.example.app", map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow})
	if err != nil {
		t.Fatalf("SetPermissions: %v", err)
	}
	calls := permissionCalls(d)
	if len(calls) != 1 || calls[0].AppID != "com.example.app" || calls[0].Permissions[driver.PermissionCamera] != driver.PermissionAllow {
		t.Fatalf("driver calls = %+v, want one for the app", calls)
	}
}

func TestSetPermissions_RejectsBadInputBeforeTheDriver(t *testing.T) {
	good := map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow}
	tests := []struct {
		name        string
		appID       string
		permissions map[driver.Permission]driver.PermissionState
		wantCode    string
	}{
		{"empty app id", "", good, diag.CodeInvalidAppID},
		{"bad app id", "bad id!", good, diag.CodeInvalidAppID},
		{"empty map", "com.example.app", nil, diag.CodeInvalidArgument},
		{"unknown name", "com.example.app", map[driver.Permission]driver.PermissionState{"bluetooth": driver.PermissionAllow}, diag.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := mock.New()
			o, _ := newTestOrchestra(d)
			err := o.SetPermissions(context.Background(), tc.appID, tc.permissions)
			if code, _, _ := diag.Describe(err); code != tc.wantCode {
				t.Fatalf("code = %q (err %v), want %q", code, err, tc.wantCode)
			}
			if len(permissionCalls(d)) != 0 {
				t.Fatal("driver SetPermissions was called")
			}
		})
	}
}

func TestSetPermissions_ReportsUnsupportedWhenTheCapabilityIsOff(t *testing.T) {
	d := mock.New()
	d.SetCapabilities(driver.Capabilities{CanSetPermissions: false})
	o, _ := newTestOrchestra(d)

	err := o.SetPermissions(context.Background(), "com.example.app", map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow})
	if code, _, _ := diag.Describe(err); code != diag.CodePermissionsUnsupported {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodePermissionsUnsupported)
	}
}

func TestLaunchAppWithOptions_PermissionsAreAppliedBeforeTheLaunch(t *testing.T) {
	o, d := newLaunchModeOrchestra()
	permissions := map[driver.Permission]driver.PermissionState{driver.PermissionLocation: driver.PermissionAllow}

	if err := o.LaunchAppWithOptions(context.Background(), launchModeAppID, LaunchOptions{Permissions: permissions}); err != nil {
		t.Fatalf("LaunchAppWithOptions: %v", err)
	}
	var order []string
	for _, call := range d.Calls {
		if call.Method == "SetPermissions" || call.Method == "LaunchApp" {
			order = append(order, call.Method)
		}
	}
	if len(order) != 2 || order[0] != "SetPermissions" || order[1] != "LaunchApp" {
		t.Fatalf("call order = %v, want SetPermissions then LaunchApp", order)
	}
}

func TestLaunchAppWithOptions_APermissionLaunchIsNeverSkippedAsARepeat(t *testing.T) {
	o, d := newLaunchModeOrchestra()
	ctx := context.Background()
	if err := o.LaunchAppWithMode(ctx, launchModeAppID, LaunchModeDefault); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	permissions := map[driver.Permission]driver.PermissionState{driver.PermissionLocation: driver.PermissionAllow}
	if err := o.LaunchAppWithOptions(ctx, launchModeAppID, LaunchOptions{Permissions: permissions}); err != nil {
		t.Fatalf("second launch: %v", err)
	}
	if got := launchCalls(d); len(got) != 2 {
		t.Fatalf("driver launches = %v, want 2", got)
	}
}

func TestLaunchAppWithOptions_AnInvalidModeSetsNoPermissions(t *testing.T) {
	o, d := newLaunchModeOrchestra()
	permissions := map[driver.Permission]driver.PermissionState{driver.PermissionLocation: driver.PermissionAllow}

	err := o.LaunchAppWithOptions(context.Background(), launchModeAppID, LaunchOptions{Mode: "bogus", Permissions: permissions})
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if len(permissionCalls(d)) != 0 {
		t.Fatal("permissions were applied before the launch options were validated")
	}
}

func TestLaunchAppWithOptions_PermissionsApplyBeforeALinkOpens(t *testing.T) {
	o, d := newLaunchModeOrchestra()
	permissions := map[driver.Permission]driver.PermissionState{driver.PermissionLocation: driver.PermissionAllow}

	err := o.LaunchAppWithOptions(context.Background(), launchModeAppID, LaunchOptions{Link: "myapp://home", Permissions: permissions})
	if err != nil {
		t.Fatalf("LaunchAppWithOptions: %v", err)
	}
	var order []string
	for _, call := range d.Calls {
		if call.Method == "SetPermissions" || call.Method == "OpenLink" {
			order = append(order, call.Method)
		}
	}
	if len(order) != 2 || order[0] != "SetPermissions" || order[1] != "OpenLink" {
		t.Fatalf("call order = %v, want SetPermissions then OpenLink", order)
	}
}

func TestLaunchAppWithOptions_AnInvalidLinkSetsNoPermissions(t *testing.T) {
	o, d := newLaunchModeOrchestra()
	permissions := map[driver.Permission]driver.PermissionState{driver.PermissionLocation: driver.PermissionAllow}

	err := o.LaunchAppWithOptions(context.Background(), launchModeAppID, LaunchOptions{Link: "no scheme", Permissions: permissions})
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if len(permissionCalls(d)) != 0 {
		t.Fatal("permissions were applied before the link was validated")
	}
}
