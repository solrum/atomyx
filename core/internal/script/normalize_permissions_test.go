package script

import (
	"strings"
	"testing"

	"dev.atomyx/internal/driver"
)

func TestParseScript_SetPermissionsForms(t *testing.T) {
	step := mustParse(t, "- setPermissions:\n    permissions:\n      camera: allow\n      location: deny\n").Steps[0].(*SetPermissionsStep)
	if step.AppID != nil {
		t.Errorf("appId = %q, want none", *step.AppID)
	}
	want := map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow, driver.PermissionLocation: driver.PermissionDeny}
	if len(step.Permissions) != len(want) {
		t.Fatalf("permissions = %v, want %v", step.Permissions, want)
	}
	for name, state := range want {
		if step.Permissions[name] != state {
			t.Errorf("permissions[%s] = %q, want %q", name, step.Permissions[name], state)
		}
	}

	named := mustParse(t, "- setPermissions:\n    appId: com.other\n    permissions:\n      photos: allow\n").Steps[0].(*SetPermissionsStep)
	if named.AppID == nil || *named.AppID != "com.other" {
		t.Errorf("appId = %v, want com.other", named.AppID)
	}
}

func TestParseScript_SetPermissionsAcceptsEveryKnownNameAndState(t *testing.T) {
	var yaml strings.Builder
	yaml.WriteString("- setPermissions:\n    permissions:\n")
	for _, p := range driver.AllPermissions() {
		yaml.WriteString("      " + string(p) + ": allow\n")
	}
	step := mustParse(t, yaml.String()).Steps[0].(*SetPermissionsStep)
	if len(step.Permissions) != len(driver.AllPermissions()) {
		t.Fatalf("parsed %d permissions, want %d", len(step.Permissions), len(driver.AllPermissions()))
	}
}

func TestParseScript_LaunchAppPermissions(t *testing.T) {
	step := mustParse(t, "- launchApp:\n    permissions:\n      location: allow\n").Steps[0].(*LaunchAppStep)
	if step.Permissions[driver.PermissionLocation] != driver.PermissionAllow {
		t.Fatalf("permissions = %v, want location allow", step.Permissions)
	}
	empty := mustParse(t, "- launchApp:\n    permissions: {}\n").Steps[0].(*LaunchAppStep)
	if empty.Permissions != nil {
		t.Errorf("empty permissions = %v, want nil", empty.Permissions)
	}
}

func TestParseScript_PermissionsRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"bare step", "- setPermissions\n", "setPermissions: expected an object"},
		{"string value", "- setPermissions: camera\n", "setPermissions: expected an object"},
		{"missing permissions", "- setPermissions:\n    appId: com.x\n", "needs a `permissions` mapping"},
		{"empty permissions", "- setPermissions:\n    permissions: {}\n", "needs at least one permission"},
		{"unknown name", "- setPermissions:\n    permissions:\n      bluetooth: allow\n", "unknown permission"},
		{"unknown name in launch", "- launchApp:\n    permissions:\n      bluetooth: allow\n", "launchApp.permissions.bluetooth: unknown permission"},
		{"unknown state", "- setPermissions:\n    permissions:\n      camera: maybe\n", "not a permission state"},
		{"state not text", "- setPermissions:\n    permissions:\n      camera: [allow]\n", "must be allow or deny"},
		{"permissions not a mapping", "- setPermissions:\n    permissions: [camera]\n", "must be a mapping"},
		{"app id not a string", "- setPermissions:\n    appId: [a]\n    permissions:\n      camera: allow\n", "setPermissions.appId: must be a string"},
		{"empty app id", "- setPermissions:\n    appId: \"\"\n    permissions:\n      camera: allow\n", "appId: must not be empty"},
		{"launch with stopApp false", "- launchApp:\n    stopApp: false\n    permissions:\n      camera: allow\n", "permissions cannot be combined with stopApp: false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScript(tc.yaml, nil)
			if err == nil {
				t.Fatalf("ParseScript accepted %q", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
