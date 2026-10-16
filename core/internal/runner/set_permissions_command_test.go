package runner

import (
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

func TestSetPermissionsCommand_HandsThePermissionsToTheDriver(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- setPermissions:\n    permissions:\n      camera: allow\n      location: deny\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if len(d.PermissionChanges) != 1 {
		t.Fatalf("got %d permission changes, want 1", len(d.PermissionChanges))
	}
	change := d.PermissionChanges[0]
	if change.AppID != "com.test" || change.Permissions[driver.PermissionCamera] != driver.PermissionAllow || change.Permissions[driver.PermissionLocation] != driver.PermissionDeny {
		t.Fatalf("got change %+v, want the script app with camera allow and location deny", change)
	}
	if got := detail(lastStep(t, result)); got != "Set camera=allow, location=deny for com.test" {
		t.Errorf("detail = %q", got)
	}
}

func TestSetPermissionsCommand_NamesTheAppWhenTheStepDoes(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- setPermissions:\n    appId: com.other\n    permissions:\n      photos: allow\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if len(d.PermissionChanges) != 1 || d.PermissionChanges[0].AppID != "com.other" {
		t.Fatalf("got changes %+v, want one for com.other", d.PermissionChanges)
	}
}

func TestSetPermissionsCommand_FailsTheStepWhenTheDeviceCannotSetPermissions(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	d.SetCapabilities(driver.Capabilities{CanSetPermissions: false})
	result := runConfigured(t, r, "appId: com.test\n---\n- setPermissions:\n    permissions:\n      camera: allow\n")
	if result.Ok {
		t.Fatal("got Ok=true, want the step to fail")
	}
	if got := lastStep(t, result); got.Code != diag.CodePermissionsUnsupported {
		t.Errorf("code = %q, want %q", got.Code, diag.CodePermissionsUnsupported)
	}
}

func TestLaunchAppCommand_AppliesPermissionsBeforeTheLaunchAndIsNeverSkipped(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- launchApp\n- launchApp:\n    permissions:\n      location: allow\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if len(d.PermissionChanges) != 1 {
		t.Fatalf("got %d permission changes, want 1", len(d.PermissionChanges))
	}
	if got := len(driverCalls(d, "LaunchApp")); got != 2 {
		t.Fatalf("got %d launches, want 2 because the permission launch is not a repeat", got)
	}
	var order []string
	for _, c := range d.Calls {
		if c.Method == "SetPermissions" || c.Method == "LaunchApp" {
			order = append(order, c.Method)
		}
	}
	if len(order) != 3 || order[1] != "SetPermissions" || order[2] != "LaunchApp" {
		t.Fatalf("call order = %v, want the permissions set just before the second launch", order)
	}
}

func TestSummarizeStep_SetPermissionsAndLaunchPermissions(t *testing.T) {
	def := mustParse(t, "appId: com.test\n---\n- setPermissions:\n    permissions:\n      camera: allow\n- setPermissions:\n    appId: com.other\n    permissions:\n      photos: deny\n- launchApp:\n    permissions:\n      location: allow\n")
	want := []string{
		`setPermissions: camera=allow`,
		`setPermissions: "com.other" photos=deny`,
		`launchApp: permissions=location=allow`,
	}
	for i, w := range want {
		if got := SummarizeStep(def.Steps[i]).Text; got != w {
			t.Errorf("summary %d = %q, want %q", i, got, w)
		}
	}
}
