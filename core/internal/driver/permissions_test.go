package driver

import (
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestValidatePermissions(t *testing.T) {
	accepted := []map[Permission]PermissionState{
		{PermissionCamera: PermissionAllow},
		{PermissionLocation: PermissionDeny, PermissionNotifications: PermissionAllow},
	}
	for _, permissions := range accepted {
		if err := ValidatePermissions(permissions); err != nil {
			t.Errorf("ValidatePermissions(%v) = %v, want nil", permissions, err)
		}
	}

	rejected := []struct {
		name        string
		permissions map[Permission]PermissionState
		want        string
	}{
		{"empty", map[Permission]PermissionState{}, "at least one"},
		{"nil", nil, "at least one"},
		{"unknown name", map[Permission]PermissionState{"bluetooth": PermissionAllow}, "not a known permission"},
		{"unknown state", map[Permission]PermissionState{PermissionCamera: "maybe"}, "not a permission state"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePermissions(tc.permissions)
			if err == nil {
				t.Fatalf("ValidatePermissions(%v) = nil, want an error", tc.permissions)
			}
			if code, message, _ := diag.Describe(err); code != diag.CodeInvalidArgument || !strings.Contains(message, tc.want) {
				t.Fatalf("got code %q message %q, want %q containing %q", code, message, diag.CodeInvalidArgument, tc.want)
			}
		})
	}
}

func TestParsePermissionAndState(t *testing.T) {
	for _, p := range AllPermissions() {
		if got, ok := ParsePermission(string(p)); !ok || got != p {
			t.Errorf("ParsePermission(%q) = %q, %v", p, got, ok)
		}
	}
	if _, ok := ParsePermission("Camera"); ok {
		t.Error("ParsePermission accepted a name with the wrong case")
	}
	for _, s := range AllPermissionStates() {
		if got, ok := ParsePermissionState(string(s)); !ok || got != s {
			t.Errorf("ParsePermissionState(%q) = %q, %v", s, got, ok)
		}
	}
	if _, ok := ParsePermissionState("unset"); ok {
		t.Error("ParsePermissionState accepted unset")
	}
}

func TestSortedPermissionsIsDeterministic(t *testing.T) {
	got := SortedPermissions(map[Permission]PermissionState{
		PermissionPhotos: PermissionAllow, PermissionCamera: PermissionDeny, PermissionLocation: PermissionAllow,
	})
	want := []Permission{PermissionCamera, PermissionLocation, PermissionPhotos}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortedPermissions = %v, want %v", got, want)
		}
	}
}
